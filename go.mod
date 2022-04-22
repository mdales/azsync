module github.com/mdales/azsync

go 1.18

replace github.com/gabriel-vasile/mimetype => ../mimetype

require (
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v0.4.0
	github.com/gabriel-vasile/mimetype v1.1.1
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v0.23.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v0.9.2 // indirect
	golang.org/x/net v0.0.0-20220421235706-1d1ef9303861 // indirect
	golang.org/x/text v0.3.7 // indirect
)
