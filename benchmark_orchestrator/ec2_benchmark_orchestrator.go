package benchmarkorchestrator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"math/big"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Octogonapus/S3Benchmark/benchmark"
	"github.com/Octogonapus/S3Benchmark/profile"
	"github.com/Octogonapus/S3Benchmark/report"
	"github.com/Octogonapus/S3Benchmark/target"
	"github.com/Octogonapus/S3Benchmark/util"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/crypto/ssh"
)

type ec2BenchmarkOrchestrator struct {
	input                *EC2BenchmarkOrchestratorInput
	benchmarks           []benchmark.Benchmark
	cfg                  *BenchmarkConfig
	ec2                  *ec2.Client
	iam                  *iam.Client
	s3                   *s3.Client
	vpcID                *string
	igwID                *string
	sgID                 *string
	subnetID             *string
	s3EndpointID         *string
	roleArn              *string
	roleName             *string
	roleInlinePolicyName *string
	insProfArn           *string
	insProfName          *string
	keyName              *string
	keyID                *string
	signer               ssh.Signer
	s3Prefixes           []netip.Prefix
	totalObjectSizeGB    int32
}

type benchmarkResult struct {
	benchmark    benchmark.Benchmark
	instanceType ec2Types.InstanceType
	report       *report.BenchmarkReport
	err          error
}

type benchmarkMetadata struct {
	InstanceType string
}

type EC2BenchmarkOrchestratorInput struct {
	AwsConfig            aws.Config
	InstanceTypes        []ec2Types.InstanceType
	WaitToInitialize     bool
	Bucket               string
	ProfilerKind         profile.ProfilerKind
	ProfileSaveDir       string
	BenchmarkConcurrency int // runs all benchmarks in parallel by default
	BenchmarkRuns        int // number of times to run each benchmark. 1 = run the benchmark once. 1 by default.
}

func NewEC2BenchmarkOrchestrator(input *EC2BenchmarkOrchestratorInput) (*ec2BenchmarkOrchestrator, error) {
	return &ec2BenchmarkOrchestrator{
		input: input,
		ec2:   ec2.NewFromConfig(input.AwsConfig),
		iam:   iam.NewFromConfig(input.AwsConfig),
		s3:    s3.NewFromConfig(input.AwsConfig),
	}, nil
}

func (o *ec2BenchmarkOrchestrator) AddBenchmark(b benchmark.Benchmark) error {
	o.benchmarks = append(o.benchmarks, b)
	return nil
}

func (o *ec2BenchmarkOrchestrator) SetUp(cfg *BenchmarkConfig) error {
	o.cfg = cfg

	sizeBytes := 0
	for _, obj := range o.cfg.ObjectSpecs {
		sizeBytes += obj.SizeBytes
	}
	o.totalObjectSizeGB = int32(math.Ceil(float64(sizeBytes) / 1e9))

	if o.cfg.WarmUpObjects {
		// TODO implement object warming
		return fmt.Errorf("object warming is not implemented")
	}

	err := os.MkdirAll(o.cfg.ResultDir, fs.ModePerm)
	if err != nil {
		return err
	}

	cidr := aws.String("10.0.0.0/16")
	vpc, err := o.ec2.CreateVpc(context.Background(), &ec2.CreateVpcInput{
		CidrBlock: cidr,
		TagSpecifications: []ec2Types.TagSpecification{{
			ResourceType: ec2Types.ResourceTypeVpc,
			Tags: []ec2Types.Tag{{
				Key:   aws.String("Name"),
				Value: o.randString(),
			}},
		}},
	})
	if err != nil {
		return err
	}
	slog.Debug("created VPC", slog.String("ID", *vpc.Vpc.VpcId))
	o.vpcID = vpc.Vpc.VpcId

	// This must be done in two requests
	_, err = o.ec2.ModifyVpcAttribute(context.Background(), &ec2.ModifyVpcAttributeInput{
		VpcId:            o.vpcID,
		EnableDnsSupport: &ec2Types.AttributeBooleanValue{Value: aws.Bool(true)},
	})
	if err != nil {
		return err
	}
	_, err = o.ec2.ModifyVpcAttribute(context.Background(), &ec2.ModifyVpcAttributeInput{
		VpcId:              o.vpcID,
		EnableDnsHostnames: &ec2Types.AttributeBooleanValue{Value: aws.Bool(true)},
	})
	if err != nil {
		return err
	}

	subnet, err := o.ec2.CreateSubnet(context.Background(), &ec2.CreateSubnetInput{
		VpcId:     o.vpcID,
		CidrBlock: cidr,
	})
	if err != nil {
		return err
	}
	slog.Debug("created subnet", slog.String("ID", *subnet.Subnet.SubnetId))
	o.subnetID = subnet.Subnet.SubnetId

	igw, err := o.ec2.CreateInternetGateway(context.Background(), &ec2.CreateInternetGatewayInput{})
	if err != nil {
		return err
	}
	slog.Debug("created internet gateway", slog.String("ID", *igw.InternetGateway.InternetGatewayId))
	o.igwID = igw.InternetGateway.InternetGatewayId

	_, err = o.ec2.AttachInternetGateway(context.Background(), &ec2.AttachInternetGatewayInput{
		InternetGatewayId: o.igwID,
		VpcId:             o.vpcID,
	})
	if err != nil {
		return err
	}

	// The VPC comes with a main route table so we don't make one
	routeTable, err := o.ec2.DescribeRouteTables(context.Background(), &ec2.DescribeRouteTablesInput{
		Filters: []ec2Types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{*o.vpcID}},
		},
	})
	if err != nil {
		return err
	}
	routeTableID := routeTable.RouteTables[0].RouteTableId

	_, err = o.ec2.CreateRoute(context.Background(), &ec2.CreateRouteInput{
		RouteTableId:         routeTableID,
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            o.igwID,
	})
	if err != nil {
		return err
	}

	regionalS3ServiceName := aws.String(fmt.Sprintf("com.amazonaws.%s.s3", o.input.AwsConfig.Region))
	s3Endpoint, err := o.ec2.CreateVpcEndpoint(context.Background(), &ec2.CreateVpcEndpointInput{
		VpcId:           o.vpcID,
		ServiceName:     regionalS3ServiceName,
		VpcEndpointType: ec2Types.VpcEndpointTypeGateway,
		RouteTableIds:   []string{*routeTableID},
	})
	if err != nil {
		return err
	}
	slog.Debug("created S3 endpoint", slog.String("ID", *s3Endpoint.VpcEndpoint.VpcEndpointId))
	o.s3EndpointID = s3Endpoint.VpcEndpoint.VpcEndpointId

	// After we add the S3 endpoint it will add a route with a prefix list containing S3 CIDRs. We need to collect these.
	routeTableAfterS3Endpoint, err := o.ec2.DescribeRouteTables(context.Background(), &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{*routeTableID},
	})
	if err != nil {
		return err
	}
	for _, route := range routeTableAfterS3Endpoint.RouteTables[0].Routes {
		if route.DestinationPrefixListId != nil {
			pl, err := o.ec2.GetManagedPrefixListEntries(context.Background(), &ec2.GetManagedPrefixListEntriesInput{
				PrefixListId: route.DestinationPrefixListId,
			})
			if err != nil {
				return err
			}
			for _, entry := range pl.Entries {
				prefix, err := netip.ParsePrefix(*entry.Cidr)
				if err != nil {
					return err
				}
				o.s3Prefixes = append(o.s3Prefixes, prefix)
			}
		}
	}

	sg, err := o.ec2.CreateSecurityGroup(context.Background(), &ec2.CreateSecurityGroupInput{
		GroupName:   o.randString(),
		Description: o.randString(),
		VpcId:       o.vpcID,
	})
	if err != nil {
		return err
	}
	slog.Debug("created security group", slog.String("ID", *sg.GroupId))
	o.sgID = sg.GroupId

	_, err = o.ec2.AuthorizeSecurityGroupIngress(context.Background(), &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: o.sgID,
		IpPermissions: []ec2Types.IpPermission{
			{
				FromPort:   aws.Int32(22),
				IpProtocol: aws.String("tcp"),
				IpRanges:   []ec2Types.IpRange{{CidrIp: aws.String("0.0.0.0/0")}},
				ToPort:     aws.Int32(22),
			},
		},
	})
	if err != nil {
		return err
	}

	assumePolicy := PolicyDocument{
		Version: "2012-10-17",
		Statement: []StatementEntry{{
			Effect:    "Allow",
			Action:    []string{"sts:AssumeRole"},
			Principal: map[string][]string{"Service": {"ec2.amazonaws.com"}},
		}},
	}
	assumePolicyDoc, err := json.Marshal(assumePolicy)
	if err != nil {
		return err
	}
	role, err := o.iam.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 o.randString(),
		AssumeRolePolicyDocument: aws.String(string(assumePolicyDoc)),
		MaxSessionDuration:       aws.Int32(int32((12 * time.Hour).Seconds())),
	})
	if err != nil {
		return err
	}
	o.roleArn = role.Role.Arn
	o.roleName = role.Role.RoleName
	slog.Debug("created role", slog.String("name", *o.roleName))

	policy := PolicyDocument{
		Version: "2012-10-17",
		Statement: []StatementEntry{
			{
				Effect:   "Allow",
				Action:   []string{"s3:GetObject", "s3:ListBucket"},
				Resource: []string{fmt.Sprintf("arn:aws:s3:::%s/*", o.input.Bucket), fmt.Sprintf("arn:aws:s3:::%s", o.input.Bucket)},
			},
		},
	}
	policyDoc, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	o.roleInlinePolicyName = aws.String("inline")
	_, err = o.iam.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
		RoleName:       role.Role.RoleName,
		PolicyName:     o.roleInlinePolicyName,
		PolicyDocument: aws.String(string(policyDoc)),
	})
	if err != nil {
		return err
	}

	insProf, err := o.iam.CreateInstanceProfile(context.Background(), &iam.CreateInstanceProfileInput{
		InstanceProfileName: o.randString(),
	})
	if err != nil {
		return err
	}
	o.insProfArn = insProf.InstanceProfile.Arn
	o.insProfName = insProf.InstanceProfile.InstanceProfileName
	slog.Debug("created instance profile", slog.String("name", *o.insProfName))

	o.iam.AddRoleToInstanceProfile(context.Background(), &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: insProf.InstanceProfile.InstanceProfileName,
		RoleName:            role.Role.RoleName,
	})

	keyPair, err := o.ec2.CreateKeyPair(context.Background(), &ec2.CreateKeyPairInput{
		KeyName:   o.randString(),
		KeyType:   ec2Types.KeyTypeEd25519,
		KeyFormat: ec2Types.KeyFormatPem,
	})
	if err != nil {
		return err
	}
	o.keyName = keyPair.KeyName
	o.keyID = keyPair.KeyPairId
	slog.Debug("created key pair", slog.String("ID", *o.keyID))
	o.signer, err = ssh.ParsePrivateKey([]byte(*keyPair.KeyMaterial))
	if err != nil {
		return err
	}

	// IAM needs a few seconds to propagate the instance profile
	time.Sleep(10 * time.Second)

	return nil
}

func (o *ec2BenchmarkOrchestrator) runBenchmark(
	resultCh chan *benchmarkResult,
	keys []string,
	b benchmark.Benchmark,
	instanceType ec2Types.InstanceType,
) {
	// Random jitter to offset each benchmark when we start lots of them at once
	nBig, err := rand.Int(rand.Reader, big.NewInt(10))
	if err != nil {
		panic(err)
	}
	n := nBig.Int64()
	time.Sleep(time.Duration(n * int64(time.Second)))

	result := &benchmarkResult{
		benchmark:    b,
		instanceType: instanceType,
	}

	resp, err := o.ec2.DescribeInstanceTypes(context.Background(), &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []ec2Types.InstanceType{instanceType},
	})
	if err != nil {
		result.err = err
		resultCh <- result
		return
	}

	instance, err := o.launchInstance(instanceType)
	if err != nil {
		result.err = err
		resultCh <- result
		return
	}
	instanceID := instance.Instances[0].InstanceId
	defer func() {
		_, err := o.ec2.TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{
			InstanceIds: []string{*instanceID},
		})
		if err != nil {
			slog.Error("failed to destroy instance", slog.String("instanceID", *instanceID), slog.String("error", err.Error()))
		}

		// Wait for the instance to be terminated, otherwise teardown can fail
		for i := 0; i < 5; i++ {
			resp, err := o.ec2.DescribeInstances(context.Background(), &ec2.DescribeInstancesInput{
				InstanceIds: []string{*instanceID},
			})
			if err == nil && len(resp.Reservations) > 0 &&
				resp.Reservations[0].Instances[0].State.Name == ec2Types.InstanceStateNameTerminated {
				break
			}
			if err != nil {
				slog.Debug("waiting for instance to finish terminating", slog.String("error", err.Error()))
			} else {
				slog.Debug("waiting for instance to finish terminating")
			}
			time.Sleep(60 * time.Second)
		}
	}()

	// Wait for the instance to finish initializing
	if o.input.WaitToInitialize {
		var status *ec2.DescribeInstanceStatusOutput
		for i := 0; i < 5; i++ {
			status, err = o.ec2.DescribeInstanceStatus(context.Background(), &ec2.DescribeInstanceStatusInput{
				InstanceIds:         []string{*instanceID},
				IncludeAllInstances: aws.Bool(true),
			})
			if err == nil && len(status.InstanceStatuses) > 0 &&
				status.InstanceStatuses[0].InstanceStatus.Status == ec2Types.SummaryStatusOk &&
				status.InstanceStatuses[0].SystemStatus.Status == ec2Types.SummaryStatusOk {
				break
			}

			if err != nil {
				slog.Debug("waiting for instance to finish initializing", slog.String("error", err.Error()))
			} else {
				slog.Debug("waiting for instance to finish initializing")
			}

			time.Sleep(60 * time.Second)
		}
		if err != nil {
			result.err = err
			resultCh <- result
			return
		}
	}

	instanceIP, err := o.getInstanceIP(instanceID)
	if err != nil {
		result.err = err
		resultCh <- result
		return
	}
	slog.Debug("instance got IP", slog.String("instanceID", *instanceID), slog.String("ip", *instanceIP))

	target := &target.SSHTarget{
		User:    aws.String("ubuntu"),
		IP:      instanceIP,
		SSHPort: 22,
		Auths:   []ssh.AuthMethod{ssh.PublicKeys(o.signer)},
	}

	err = o.waitForTargetReachable(target)
	if err != nil {
		slog.Error("instance is not reachable", slog.String("instanceID", *instanceID), slog.String("error", err.Error()))
		result.err = err
		resultCh <- result
		return
	}

	err = o.configureForRootLogin(target)
	if err != nil {
		slog.Error("failed to configure target for root login", slog.String("instanceID", *instanceID), slog.String("error", err.Error()))
		result.err = err
		resultCh <- result
		return
	}
	target.User = aws.String("root")

	ctx := &benchmark.BenchmarkContext{
		Target:            target,
		DesiredThroughput: *resp.InstanceTypes[0].NetworkInfo.NetworkCards[0].BaselineBandwidthInGbps,
		Bucket:            o.input.Bucket,
		Keys:              keys,
		Region:            o.input.AwsConfig.Region,
	}

	br := benchmark.NewBenchmarkRunner(b, o.input.ProfilerKind, o.input.ProfileSaveDir, o.input.BenchmarkRuns)
	err = br.SetUp(ctx, o.s3Prefixes)
	if err != nil {
		slog.Error("benchmark setup failed", slog.String("benchmarkName", b.GetName()), slog.String("error", err.Error()))
		result.err = err
		resultCh <- result
		return
	}

	result.report = br.Run()
	resultCh <- result
}

func (o *ec2BenchmarkOrchestrator) RunBenchmarks() (*Report, error) {
	keys := []string{}
	for _, obj := range o.cfg.ObjectSpecs {
		keys = append(keys, obj.Key)
	}

	ntotal := len(o.benchmarks) * len(o.input.InstanceTypes)
	resultCh := make(chan *benchmarkResult, ntotal)

	concurrency := o.input.BenchmarkConcurrency
	if concurrency == 0 {
		// unlimited
		wg := &sync.WaitGroup{}
		for _, b := range o.benchmarks {
			for _, instanceType := range o.input.InstanceTypes {
				wg.Add(1)
				go func() {
					defer wg.Done()
					o.runBenchmark(resultCh, keys, b, instanceType) // gopls complains but we use 1.22
				}()
			}
		}
		wg.Wait()
	} else {
		pool := pond.New(concurrency, 0, pond.MinWorkers(concurrency))
		for _, b := range o.benchmarks {
			for _, instanceType := range o.input.InstanceTypes {
				pool.Submit(func() {
					o.runBenchmark(resultCh, keys, b, instanceType)
				})
			}
		}
		pool.StopAndWait()
	}

	close(resultCh)

	rep := &Report{
		Config:  o.cfg,
		Reports: []*report.BenchmarkReport{},
	}
	for result := range resultCh {
		meta := benchmarkMetadata{
			InstanceType: string(result.instanceType),
		}
		if result.err == nil {
			result.report.Metadata = append(result.report.Metadata, meta)
			rep.Reports = append(rep.Reports, result.report)
		} else {
			slog.Error("benchmark failed",
				slog.String("error", result.err.Error()),
				slog.String("benchmark", result.benchmark.GetName()),
				slog.String("instanceType", string(result.instanceType)),
			)
			rep.Reports = append(rep.Reports, &report.BenchmarkReport{
				Name:     result.benchmark.GetName(),
				Metadata: []any{meta},
				Error:    result.err.Error(),
			})
		}
	}
	return rep, nil
}

func ParseNetworkPerformance(perf string) (int, error) {
	parts := strings.Fields(perf)
	unit := parts[len(parts)-1]
	num := parts[len(parts)-2]
	if unit == "Gigabit" {
		return strconv.Atoi(num)
	}
	return 0, fmt.Errorf("unknown unit: %s", unit)
}

func (o *ec2BenchmarkOrchestrator) launchInstance(instanceType ec2Types.InstanceType) (*ec2.RunInstancesOutput, error) {
	var resp *ec2.RunInstancesOutput
	var err error
	for i := 0; i < 5; i++ {
		resp, err = o.ec2.RunInstances(context.Background(), &ec2.RunInstancesInput{
			MinCount:     aws.Int32(1),
			MaxCount:     aws.Int32(1),
			EbsOptimized: aws.Bool(true),
			ImageId:      aws.String("ami-05fb0b8c1424f266b"), // ubuntu 22.04 from canonical
			BlockDeviceMappings: []ec2Types.BlockDeviceMapping{
				{
					DeviceName: aws.String("/dev/sda1"),
					// TODO maybe expose EBS volume eventually? for our testing it is not necessary but there are higher throughput options
					Ebs: &ec2Types.EbsBlockDevice{
						VolumeSize:          aws.Int32(max(o.totalObjectSizeGB+10, 32)),
						VolumeType:          ec2Types.VolumeTypeGp3,
						Iops:                aws.Int32(16000),
						Throughput:          aws.Int32(1000),
						DeleteOnTermination: aws.Bool(true),
						Encrypted:           aws.Bool(true),
					},
				},
			},
			InstanceType: instanceType,
			KeyName:      o.keyName,
			NetworkInterfaces: []ec2Types.InstanceNetworkInterfaceSpecification{
				{
					DeviceIndex:              aws.Int32(0),
					AssociatePublicIpAddress: aws.Bool(true),
					Groups:                   []string{*o.sgID},
					SubnetId:                 o.subnetID,
					DeleteOnTermination:      aws.Bool(true),
				},
			},
			IamInstanceProfile: &ec2Types.IamInstanceProfileSpecification{Name: o.insProfName},
		})
		if err == nil {
			slog.Debug("launched instance", slog.String("instanceID", *resp.Instances[0].InstanceId))
			return resp, err
		}
		slog.Debug("waiting to launch instance", slog.String("error", err.Error()))
		time.Sleep(60 * time.Second)
	}
	return nil, fmt.Errorf("failed to launch instance: %w", err)
}

func (o *ec2BenchmarkOrchestrator) getInstanceIP(instanceID *string) (*string, error) {
	for i := 0; i < 10; i++ {
		resp, err := o.ec2.DescribeInstances(context.Background(), &ec2.DescribeInstancesInput{
			InstanceIds: []string{*instanceID},
		})
		if err != nil {
			return nil, err
		}

		ip := resp.Reservations[0].Instances[0].PublicIpAddress
		if ip != nil {
			return ip, nil
		}

		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("failed to get instance %s IP", *instanceID)
}

func (o *ec2BenchmarkOrchestrator) waitForTargetReachable(target target.Target) error {
	for i := 0; i < 6*5; i++ {
		buf, err := target.RunCommand("whoami")
		if err != nil || strings.TrimSpace(string(buf)) != "ubuntu" {
			slog.Debug("target reachability check failed", slog.String("error", err.Error()))
			time.Sleep(10 * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("timed out waiting for target to be reachable")
}

func (o *ec2BenchmarkOrchestrator) TearDown() error {
	if o.keyID != nil {
		_, err := o.ec2.DeleteKeyPair(context.Background(), &ec2.DeleteKeyPairInput{
			KeyPairId: o.keyID,
		})
		if err != nil {
			slog.Error("DeleteKeyPair failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted key pair", slog.String("ID", *o.keyID))
		}
	}

	if o.insProfName != nil {
		_, err := o.iam.RemoveRoleFromInstanceProfile(context.Background(), &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: o.insProfName,
			RoleName:            o.roleName,
		})
		if err != nil {
			slog.Debug("RemoveRoleFromInstanceProfile failed", slog.String("error", err.Error()))
		}

		_, err = o.iam.DeleteInstanceProfile(context.Background(), &iam.DeleteInstanceProfileInput{
			InstanceProfileName: o.insProfName,
		})
		if err != nil {
			slog.Error("DeleteInstanceProfile failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted instance profile", slog.String("name", *o.insProfName))
		}
	}

	if o.roleName != nil {
		_, err := o.iam.DeleteRolePolicy(context.Background(), &iam.DeleteRolePolicyInput{
			RoleName:   o.roleName,
			PolicyName: o.roleInlinePolicyName,
		})
		if err != nil {
			slog.Debug("DeleteRolePolicy failed", slog.String("error", err.Error()))
		}

		_, err = o.iam.DeleteRole(context.Background(), &iam.DeleteRoleInput{
			RoleName: o.roleName,
		})
		if err != nil {
			slog.Error("DeleteRole failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted role", slog.String("name", *o.roleName))
		}
	}

	if o.sgID != nil {
		_, err := o.ec2.DeleteSecurityGroup(context.Background(), &ec2.DeleteSecurityGroupInput{
			GroupId: o.sgID,
		})
		if err != nil {
			slog.Error("DeleteSecurityGroup failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted security group", slog.String("ID", *o.sgID))
		}
	}

	if o.igwID != nil {
		_, err := o.ec2.DetachInternetGateway(context.Background(), &ec2.DetachInternetGatewayInput{
			VpcId:             o.vpcID,
			InternetGatewayId: o.igwID,
		})
		if err != nil {
			slog.Error("DetachInternetGateway failed", slog.String("error", err.Error()))
		}

		_, err = o.ec2.DeleteInternetGateway(context.Background(), &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: o.igwID,
		})
		if err != nil {
			slog.Error("DeleteInternetGateway failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted internet gateway", slog.String("ID", *o.igwID))
		}
	}

	if o.s3EndpointID != nil {
		_, err := o.ec2.DeleteVpcEndpoints(context.Background(), &ec2.DeleteVpcEndpointsInput{
			VpcEndpointIds: []string{*o.s3EndpointID},
		})
		if err != nil {
			slog.Error("DeleteVpcEndpoints failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted S3 endpoint", slog.String("ID", *o.s3EndpointID))
		}
	}

	if o.subnetID != nil {
		_, err := o.ec2.DeleteSubnet(context.Background(), &ec2.DeleteSubnetInput{
			SubnetId: o.subnetID,
		})
		if err != nil {
			slog.Error("DeleteSubnet failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted subnet", slog.String("ID", *o.subnetID))
		}
	}

	if o.vpcID != nil {
		_, err := o.ec2.DeleteVpc(context.Background(), &ec2.DeleteVpcInput{
			VpcId: o.vpcID,
		})
		if err != nil {
			slog.Error("DeleteVpc failed", slog.String("error", err.Error()))
		} else {
			slog.Debug("deleted VPC", slog.String("ID", *o.vpcID))
		}
	}

	return fmt.Errorf("not implemented")
}

func (o *ec2BenchmarkOrchestrator) randString() *string {
	return aws.String(fmt.Sprintf("benchmark-%s", util.Randstring(8)))
}

func (o *ec2BenchmarkOrchestrator) configureForRootLogin(target *target.SSHTarget) error {
	_, err := target.RunCommand("sudo sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/g' /etc/ssh/sshd_config")
	if err != nil {
		return fmt.Errorf("failed to change sshd_config: %w", err)
	}
	_, err = target.RunCommand("sudo sed -i -e 's/.*exit 142\" \\(.*$\\)/\\1/' /root/.ssh/authorized_keys")
	if err != nil {
		return fmt.Errorf("failed to change authorized_keys: %w", err)
	}
	_, err = target.RunCommand("sudo systemctl restart ssh")
	if err != nil {
		return fmt.Errorf("failed to restart ssh: %w", err)
	}
	return nil
}
