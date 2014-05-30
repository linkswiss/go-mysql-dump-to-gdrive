# MySql Dump to gDrive

Simple Go app to dump your MySql db on Google Drive

## Help
```
go run go-mysql-dump-to-gdrive --help
```

## Arguments
* -cache-file="cache.json": Cache File Name (default cache.json)
*-client-id="": Google API OAuth Client ID
* -client-secret="": Google API OAuth Client Secret
* -code="": Authorization Code
* -db="": Database Name
* -db-host="localhost": Name of your MySql dump HOST
* -db-user="": Name of your MySql dump USER
* -dump-all=false: If set script dump all MySql Databases
* -gdrive-folder-id="": Backup Folder ID
* -gzip=false: If set Gzip Compression enabled
* -keep-last=168h0m0s: time.Duration (Keep last backups i.e. 10m, 24h (default 168h, last 7 days))
* -log-dir="/var/log": Log directory (default /var/log)
* -tmp-dir="/tmp": Temp directory (default /tmp)