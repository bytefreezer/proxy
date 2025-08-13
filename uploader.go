package bytefreezer

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/n0needt0/go-goodies/log"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/services"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

type Uploader struct {
	Services   *services.Services
	Config     *config.Config
	quit       chan bool
	wg         sync.WaitGroup
	uploadChan chan UploadTask
}

func NewUploader(services *services.Services, config *config.Config, uploadTaskChan chan UploadTask) *Uploader {
	return &Uploader{
		Services:   services,
		Config:     config,
		quit:       make(chan bool),
		uploadChan: uploadTaskChan,
		wg:         sync.WaitGroup{},
	}
}

func (u *Uploader) Shutdown() error {
	log.Info("S3 Uploader shutting down")
	u.quit <- true
	u.wg.Wait()
	return nil
}

func (u *Uploader) Start() error {
	s3Client, err := minio.New(u.Config.S3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(u.Config.S3.AccessKey, u.Config.S3.SecretKey, ""),
		Secure: u.Config.S3.Ssl,
	})
	if err != nil {
		log.Fatalf("Failed to create S3 client: %v", err)
	}

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()

		// Recover from panic and log error
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("Recovered from panic in uploader goroutine: %v", r)
			}
		}()

		for {
			select {
			case filePath := <-u.uploadChan:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Errorf("Recovered from panic while handling file %s: %v", filePath, r)
						}
					}()

					if u.Config.Bytefreezer.EnableParquetOutput {
						u.uploadParquet(filePath.DataFile, filePath.SchemaFile, s3Client, filePath.Timestamp)
					}
					if u.Config.Bytefreezer.EnableJsonOutput {
						u.uploadJson(filePath.DataFile, s3Client, filePath.Timestamp)
					}
					if !u.Config.Bytefreezer.KeepJsonSource {
						if err := os.Remove(filePath.DataFile); err != nil {
							log.Warnf("Failed to remove source file %s: %v", filePath, err)
						}
					}
				}()
			case <-u.quit:
				log.Info("Uploader received shutdown signal")
				return
			}
		}
	}()

	return nil
}

func (u *Uploader) uploadJson(filePath string, s3Client *minio.Client, timestamp string) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Errorf("Failed to open file for upload: %v", err)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Errorf("Failed to stat file: %v", err)
		return
	}

	var reader io.Reader = file
	objectKey := fmt.Sprintf("%s/%s/json/%s.ndjson", u.Config.Bytefreezer.Token, time.Now().Format("2006-01-02"), timestamp)
	contentType := "application/x-ndjson"

	if u.Config.S3.Compression {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, err = io.Copy(gz, file)
		gz.Close()
		if err != nil {
			log.Errorf("Failed to gzip file: %v", err)
			return
		}
		tempBuf := buf.Bytes()
		reader = bytes.NewReader(tempBuf)
		fileInfo = &fakeFileInfo{size: int64(len(tempBuf))}
		objectKey += ".gz"
		contentType = "application/gzip"
	}

	_, err = s3Client.PutObject(context.Background(), u.Config.S3.BucketName, objectKey, reader, fileInfo.Size(), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Errorf("Failed to upload file to S3: %v", err)
	} else {
		log.Infof("Uploaded file %s to S3 successfully", objectKey)
	}
}

func (u *Uploader) uploadParquet(filePath string, schemaPath string, s3Client *minio.Client, timestamp string) {

	data, err := os.ReadFile(schemaPath)
	if err != nil {
		log.Errorf("Failed to read schema file: %v", err)
		return
	}

	buf := *bytes.NewBuffer(data)

	// Open output Parquet file
	outPath := filePath + ".parquet"
	fw, err := local.NewLocalFileWriter(outPath)
	if err != nil {
		log.Errorf("Failed to create Parquet file: %v", err)
		return
	}
	defer fw.Close()

	pw, err := writer.NewJSONWriter(buf.String(), fw, 4)
	if err != nil {
		log.Errorf("Failed to create Parquet JSON writer: %v", err)
		return
	}

	pw.CompressionType = parquet.CompressionCodec_GZIP

	// Open and stream input JSON lines
	src, err := os.Open(filePath)
	if err != nil {
		log.Errorf("Failed to open source file for Parquet conversion: %v", err)
		return
	}
	defer src.Close()

	scanner := bufio.NewScanner(src)
	rowCount := 0
	for scanner.Scan() {
		line := scanner.Text() // string
		if err := pw.Write(line); err != nil {
			log.Errorf("Error writing record to Parquet: %v", err)
			continue
		}
		rowCount++
	}
	if err := scanner.Err(); err != nil {
		log.Errorf("Error reading source file: %v", err)
	}

	if rowCount == 0 {
		log.Warn("No records written to Parquet file")
	} else {
		log.Debugf("Wrote %d rows to %s", rowCount, outPath)
	}

	if err := pw.WriteStop(); err != nil {
		log.Errorf("Error during WriteStop: %v", err)
		return
	}

	// Upload to S3
	stat, err := os.Stat(outPath)
	if err != nil {
		log.Errorf("Failed to stat Parquet file: %v", err)
		return
	}
	pfile, err := os.Open(outPath)
	if err != nil {
		log.Errorf("Failed to open Parquet file for upload: %v", err)
		return
	}
	defer pfile.Close()

	objectKey := fmt.Sprintf("%s/%s/parquet/%s.parquet", u.Config.Bytefreezer.Token, time.Now().Format("2006-01-02"), timestamp)
	_, err = s3Client.PutObject(context.Background(), u.Config.S3.BucketName, objectKey, pfile, stat.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		log.Errorf("Failed to upload Parquet file to S3: %v", err)
	} else {
		log.Infof("Uploaded Parquet file %s to S3 successfully", objectKey)
	}

	if !u.Config.Bytefreezer.KeepParquetSource {
		_ = os.Remove(outPath)
	}
}

type fakeFileInfo struct {
	size int64
}

func (f *fakeFileInfo) Name() string       { return "" }
func (f *fakeFileInfo) Size() int64        { return f.size }
func (f *fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f *fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f *fakeFileInfo) IsDir() bool        { return false }
func (f *fakeFileInfo) Sys() interface{}   { return nil }
