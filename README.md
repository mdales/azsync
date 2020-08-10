azsync
======

A simple tool to let me duplicate a local directory to an Azure blob container. Unlike the standard az tool batch approach this will only upload files if they've been modified more recently and are actually different in content.

Usage:

```azsync [config file] [directory to sync]```

You need to provide two arguments. The first is a configuration JSON file that specifies your Azure details:

```
{
    "accountName": "myAzureStorageAccount",
    "accountKey": "asojdjoasidu209jdladjqawdjlasjoiq2jdolawjlaw/",
    "containerName": "$web",
}
```

The second argument is the directory that you want to have the Azure container duplicate.