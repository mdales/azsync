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
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
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

func getAzureContainerClient(account azureAccountInfo) (azblob.ContainerClient, error) {
	cred, err := azblob.NewSharedKeyCredential(account.Name, account.Key)
	if err != nil {
		return azblob.ContainerClient{}, err
	}

	service, err := azblob.NewServiceClientWithSharedKey(fmt.Sprintf("https://%s.blob.core.windows.net/", account.Name), cred, nil)
    if err != nil {
        return azblob.ContainerClient{}, err
    }

	containerClient := service.NewContainerClient(account.Container)

	// Just check this client works - don't care about the results, just give
	// us some confidence that we got the creds and names right
	ctx := context.Background()
	_, err = containerClient.GetProperties(ctx, nil)
	if err != nil {
	    return azblob.ContainerClient{}, err
	}

    return containerClient, nil
}

func getAzureInfo(container azblob.ContainerClient) (map[string]azblob.BlobPropertiesInternal, error) {

    // The sample code says to use nil here, but that causes a crash currently
    options := azblob.ContainerListBlobFlatSegmentOptions{}
	pager := container.ListBlobsFlat(&options)
	if pager.Err() != nil {
	    return nil, pager.Err()
	}

    ctx := context.Background()
	info := make(map[string]azblob.BlobPropertiesInternal)

	for pager.NextPage(ctx) {
		resp := pager.PageResponse()

		for _, item := range resp.ContainerListBlobFlatSegmentResult.Segment.BlobItems {
		    if item.Name != nil && item.Properties != nil {
    			info[*item.Name] = *item.Properties
    		}
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

func buildOperationList(uploadPath string, azureInfo map[string]azblob.BlobPropertiesInternal) ([]azureOperation, error) {

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
		    if localInfo.ModTime().After(*remoteInfo.LastModified) {
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

func executeOperations(client azblob.ContainerClient, uploadPath string, operations []azureOperation) error {

	ctx := context.Background()

	for _, operation := range operations {

    	blobClient := client.NewBlockBlobClient(operation.Path)

		switch operation.Operation {
		case azureOperationTypeUpload:
			fullPath := path.Join(uploadPath, operation.Path)

			mime, err := mimetype.DetectFile(fullPath)
			fileMimeType := mime.String()
			// an override for CSS, as CSS doesn't have a regular format to let us detect it
			if path.Ext(operation.Path) == ".css" {
				fileMimeType = "text/css"
			}

			f, err := os.Open(fullPath)
			if err != nil {
				return err
			}
			headers := azblob.BlobHTTPHeaders{BlobContentType: &fileMimeType}
			options := azblob.UploadBlockBlobOptions{HTTPHeaders: &headers}
			_, err = blobClient.Upload(ctx, f, &options)
			f.Close()
			if err != nil {
				return err
			}

			log.Printf("Uploaded %s", operation.Path)

		case azureOperationTypeDelete:

			_, err := blobClient.Delete(ctx, nil)
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

	client, err := getAzureContainerClient(account)
	if err != nil {
		log.Fatalf("Failed to create container client: %v", err)
	}

	azInfo, err := getAzureInfo(client)
	if err != nil {
		log.Fatalf("Failed to fetch info from Azure: %v", err)
	}

	operations, err := buildOperationList(os.Args[2], azInfo)
	if err != nil {
		log.Fatalf("Failed to build operation list: %v", err)
	}

	err = executeOperations(client, os.Args[2], operations)
	if err != nil {
		log.Fatalf("Failed to execute operations: %v", err)
	}
}
