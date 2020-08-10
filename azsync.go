package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/gabriel-vasile/mimetype"
)

type azureAccountInfo struct {
	Name      string `json:"accountName"`
	Key       string `json:"accountKey"`
	Container string `json:"containerName"`
}

type azureOperationType string

const (
	azureOperationTypeUpload azureOperationType = "UPLOAD"
	azureOperationTypeDelete                    = "DELETE"
)

type azureOperation struct {
	Operation azureOperationType
	Path      string
}

func loadAzureAccountInfo(configFilePath string) (azureAccountInfo, error) {
	f, err := os.Open(configFilePath)
	if err != nil {
		return azureAccountInfo{}, err
	}
	defer f.Close()

	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return azureAccountInfo{}, err
	}

	accountInfo := azureAccountInfo{}
	err = json.Unmarshal(bytes, &accountInfo)
	return accountInfo, err
}

func getAzureContainerURL(account azureAccountInfo) (azblob.ContainerURL, error) {
	credential, err := azblob.NewSharedKeyCredential(account.Name, account.Key)
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	pipeline := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	url, _ := url.Parse(fmt.Sprintf("https://%s.blob.core.windows.net", account.Name))
	serviceURL := azblob.NewServiceURL(*url, pipeline)

	return serviceURL.NewContainerURL(account.Container), nil
}

func getAzureInfo(containerURL azblob.ContainerURL) (map[string]azblob.BlobProperties, error) {

	ctx := context.Background()

	info := make(map[string]azblob.BlobProperties)

	for marker := (azblob.Marker{}); marker.NotDone(); {
		listBlob, err := containerURL.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{})
		if err != nil {
			return nil, err
		}
		marker = listBlob.NextMarker

		for _, item := range listBlob.Segment.BlobItems {
			info[item.Name] = item.Properties
		}
	}
	return info, nil
}

func fileHash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, err
	}

	return hash.Sum(nil)[:16], nil
}

func buildOperationList(uploadPath string, azureInfo map[string]azblob.BlobProperties) ([]azureOperation, error) {

	operations := make([]azureOperation, 0)

	err := filepath.Walk(uploadPath, func(path string, localInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if localInfo.IsDir() {
			return nil
		}

		localPath := strings.TrimPrefix(path, uploadPath)

		if remoteInfo, ok := azureInfo[localPath]; ok {
			if localInfo.ModTime().After(remoteInfo.LastModified) {
				localContentMD5, err := fileHash(path)
				if err != nil {
					return err
				}

				if bytes.Compare(localContentMD5, remoteInfo.ContentMD5) != 0 {
					operations = append(operations, azureOperation{Operation: azureOperationTypeUpload, Path: localPath})
				}
			}
			// remove so we can work out what's in azure but not local
			delete(azureInfo, localPath)
		} else {
			operations = append(operations, azureOperation{Operation: azureOperationTypeUpload, Path: localPath})
		}

		return nil
	})

	// anything not removed above is on Azure and not local, so needs deleted
	for path := range azureInfo {
		operations = append(operations, azureOperation{Operation: azureOperationTypeDelete, Path: path})
	}

	return operations, err
}

func executeOperations(containerURL azblob.ContainerURL, uploadPath string, operations []azureOperation) error {

	ctx := context.Background()

	for _, operation := range operations {

		blobURL := containerURL.NewBlockBlobURL(operation.Path)

		switch operation.Operation {
		case azureOperationTypeUpload:
			fullPath := path.Join(uploadPath, operation.Path)

			mime, err := mimetype.DetectFile(fullPath)

			f, err := os.Open(fullPath)
			if err != nil {
				return err
			}
			_, err = blobURL.Upload(ctx, f, azblob.BlobHTTPHeaders{ContentType: mime.String()}, azblob.Metadata{}, azblob.BlobAccessConditions{})
			f.Close()
			if err != nil {
				return err
			}

			log.Printf("Uploaded %s", operation.Path)

		case azureOperationTypeDelete:

			_, err := blobURL.Delete(ctx, azblob.DeleteSnapshotsOptionNone, azblob.BlobAccessConditions{})
			if err != nil {
				return err
			}
			log.Printf("Deleted %s", operation.Path)
		}
	}
	return nil
}

func main() {

	if len(os.Args) != 3 {
		log.Fatal("Needs two arguments: [azure config] [local path]")
	}

	account, err := loadAzureAccountInfo(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to load account info: %v", err)
	}

	containerURL, err := getAzureContainerURL(account)
	if err != nil {
		log.Fatalf("Failed to get container URL: %v", err)
	}

	azInfo, err := getAzureInfo(containerURL)
	if err != nil {
		log.Fatalf("Failed to fetch info from Azure: %v", err)
	}

	operations, err := buildOperationList(os.Args[2], azInfo)
	if err != nil {
		log.Fatalf("Failed to build operation list: %v", err)
	}

	err = executeOperations(containerURL, os.Args[2], operations)
	if err != nil {
		log.Fatalf("Failed to execute operations: %v", err)
	}
}
