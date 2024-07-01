package objectprovider

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/schollz/progressbar/v3"
)

type s3ObjectProvider struct {
	input   *S3ObjectProviderInput
	s3      *s3.Client
	objects []*ObjectSpec
}

type S3ObjectProviderInput struct {
	AwsConfig         aws.Config
	Bucket            string
	UploadConcurrency int
}

func NewS3ObjectProvider(input *S3ObjectProviderInput) ObjectProvider {
	return &s3ObjectProvider{
		input: input,
		s3:    s3.NewFromConfig(input.AwsConfig),
	}
}

func (o *s3ObjectProvider) SetObjects(objects []*ObjectSpec) {
	o.objects = objects
}

func (o *s3ObjectProvider) GetObjects() []*ObjectSpec {
	return o.objects
}

func (o *s3ObjectProvider) MakeObjects() error {
	slog.Info("uploading objects", slog.String("bucket", o.input.Bucket))
	uploader := manager.NewUploader(s3.NewFromConfig(o.input.AwsConfig), func(u *manager.Uploader) {
		u.PartSize = 1024 * 1024 * 10
	})
	errChan := make(chan error, len(o.objects))
	pool := pond.New(o.input.UploadConcurrency, 0, pond.MinWorkers(o.input.UploadConcurrency))
	p := progressbar.Default(int64(len(o.objects)), "Uploading objects:")
	for _, obj := range o.objects {
		pool.Submit(func() {
			defer p.Add(1)

			// buf := make([]byte, obj.SizeBytes)
			// _, err := rand.Read(buf)
			// if err != nil {
			// 	slog.Error("failed to generate random object data", slog.String("error", err.Error()))
			// 	errChan <- err
			// 	return
			// }

			pr, pw := io.Pipe()
			go func() {
				totalWritten := 0
				for totalWritten < obj.SizeBytes {
					remaining := obj.SizeBytes - totalWritten
					if remaining > int(uploader.PartSize) {
						remaining = int(uploader.PartSize)
					}
					buf := make([]byte, remaining)
					_, err := rand.Read(buf)
					if err != nil {
						pw.CloseWithError(fmt.Errorf("failed to generate random object data: %w", err))
						return
					}
					n, err := pw.Write(buf)
					if err != nil {
						pw.CloseWithError(fmt.Errorf("failed to write random object data: %w", err))
						return
					}
					totalWritten += n
				}
				pw.Close()
			}()

			_, err := uploader.Upload(context.Background(), &s3.PutObjectInput{
				Bucket: &o.input.Bucket,
				Key:    &obj.Key,
				Body:   pr,
			})
			if err != nil {
				slog.Error("failed to upload S3 object: ", slog.String("error", err.Error()))
				errChan <- err
				return
			}
		})
	}
	pool.StopAndWait()
	p.Finish()

	select {
	case err := <-errChan:
		return fmt.Errorf("some S3 objects failed to upload: %w", err)
	default:
		slog.Info("done uploading", slog.String("bucket", o.input.Bucket))
		return nil
	}
}

func (o *s3ObjectProvider) SetUp() error {
	_, err := o.s3.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: &o.input.Bucket,
		ACL:    s3Types.BucketCannedACLPrivate,
		CreateBucketConfiguration: &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(o.input.AwsConfig.Region),
		},
	})
	var e *s3Types.BucketAlreadyOwnedByYou
	if errors.As(err, &e) {
		// this is fine, we'll just upload to it
		slog.Debug("bucket already exists", slog.String("name", o.input.Bucket))
		return nil
	} else if err != nil {
		return err
	}
	slog.Debug("created bucket", slog.String("name", o.input.Bucket))
	return nil
}

func (o *s3ObjectProvider) TearDown() error {
	pool := pond.New(32, 0, pond.MinWorkers(32))
	p := progressbar.Default(int64(len(o.objects)), "Deleting objects:")
	for {
		objs, err := o.s3.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
			Bucket: &o.input.Bucket,
		})
		if err != nil {
			slog.Error("not deleting objects in bucket because ListObjectsV2 failed", slog.String("error", err.Error()))
			break
		}
		for _, obj := range objs.Contents {
			pool.Submit(func() {
				defer p.Add(1)
				o.s3.DeleteObject(context.Background(), &s3.DeleteObjectInput{
					Bucket: &o.input.Bucket,
					Key:    obj.Key,
				})
			})
		}
		if !*objs.IsTruncated {
			break
		}
	}
	pool.StopAndWait()
	p.Finish()

	_, err := o.s3.DeleteBucket(context.Background(), &s3.DeleteBucketInput{
		Bucket: &o.input.Bucket,
	})
	if err != nil {
		slog.Error("DeleteBucket failed", slog.String("error", err.Error()))
	} else {
		slog.Debug("deleted bucket", slog.String("name", o.input.Bucket))
	}
	return nil
}

func (o *s3ObjectProvider) GetBucket() string {
	return o.input.Bucket
}
