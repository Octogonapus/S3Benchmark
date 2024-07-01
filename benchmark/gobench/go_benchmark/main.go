package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Input struct {
	Bucket              string
	ObjectsPath         string
	DownloadStrategy    string
	DownloadConcurrency int
	PartSize            int
	PartConcurrency     int
	Repeats             int
}

type Output struct {
	TotalTimeSec float64
}

func main() {
	inputStr := os.Args[1]
	input := Input{}
	err := json.Unmarshal([]byte(inputStr), &input)
	if err != nil {
		panic(err)
	}

	objsBuf, err := os.ReadFile(input.ObjectsPath)
	if err != nil {
		panic(err)
	}

	objects := []string{}
	err = json.Unmarshal(objsBuf, &objects)
	if err != nil {
		panic(err)
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithEC2IMDSRegion())
	if err != nil {
		panic(err)
	}

	s3Client := s3.NewFromConfig(cfg)

	output := Output{}

	if input.DownloadStrategy == "parts" {
		downloader := manager.NewDownloader(s3Client, func(d *manager.Downloader) {
			d.PartSize = int64(input.PartSize)
			d.Concurrency = input.PartConcurrency
		})
		pool := pond.New(input.DownloadConcurrency, 0, pond.MinWorkers(input.DownloadConcurrency))
		tstart := time.Now()
		for i := 0; i < input.Repeats; i++ {
			for _, obj := range objects {
				if len(obj) == 0 {
					continue
				}
				pool.Submit(func() {
					head, err := s3Client.HeadObject(context.Background(), &s3.HeadObjectInput{
						Bucket: &input.Bucket,
						Key:    &obj,
					})
					if err != nil {
						panic(err)
					}
					buf := make([]byte, *head.ContentLength) // it is very important to preallocate otherwise performance is TERRIBLE
					wr := manager.NewWriteAtBuffer(buf)
					_, err = downloader.Download(context.Background(), wr, &s3.GetObjectInput{
						Bucket: &input.Bucket,
						Key:    &obj,
					})
					if err != nil {
						panic(err)
					}
				})
			}
		}
		pool.StopAndWait()
		output.TotalTimeSec = time.Since(tstart).Seconds()
	} else {
		pool := pond.New(input.DownloadConcurrency, 0, pond.MinWorkers(input.DownloadConcurrency))
		tstart := time.Now()
		for i := 0; i < input.Repeats; i++ {
			for _, obj := range objects {
				if len(obj) == 0 {
					continue
				}
				pool.Submit(func() {
					resp, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
						Bucket: &input.Bucket,
						Key:    &obj,
					})
					if err != nil {
						panic(err)
					}
					// Reading the bytes is very important for an accurate test, otherwise not all data is downloaded
					buf := bytes.NewBuffer([]byte{})
					_, err = buf.ReadFrom(resp.Body)
					if err != nil {
						panic(err)
					}
				})
			}
		}
		pool.StopAndWait()
		output.TotalTimeSec = time.Since(tstart).Seconds()
	}

	outBuf, err := json.Marshal(output)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(outBuf))
}
