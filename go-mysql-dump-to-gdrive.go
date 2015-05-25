package main

import (
	"bytes"
	"code.google.com/p/google-api-go-client/drive/v2"
	"compress/gzip"
	"flag"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"time"
)

// Set the command line arguments
var (
	oAuthSecretFile = flag.String("secret-file", "client_secret.json", "Secret File (default client_secret.json)")
	gDriveFolderId  = flag.String("gdrive-folder-id", "", "Google Drive Backup Folder ID")
	mysqlUser       = flag.String("db-user", "", "Name of your MySql dump USER")
	mysqlHost       = flag.String("db-host", "localhost", "Name of your MySql dump HOST")
	mysqlDb         = flag.String("db", "", "Database Name")
	allDatabase     = flag.Bool("dump-all", false, "If set script dump all MySql Databases")
	backupDuration  = flag.Duration("keep-last", 168*time.Hour, "time.Duration (Keep last backups i.e. 10m, 24h (default 168h, last 7 days))")
	tmpDir          = flag.String("tmp-dir", "/tmp", "Temp directory (default /tmp)")
	logDir          = flag.String("log-dir", "/var/log", "Log directory (default /var/log)")
	gzipEnable      = flag.Bool("gzip", false, "If set Gzip Compression Enabled")
)

// Uploads a file to Google Drive
func main() {

	// Get command line arguments
	flag.Parse()

	// Get the hostname
	hostname, err := os.Hostname()

	filename := ""
	filetype := "text/plain"
	now := time.Now().Format(time.RFC3339)

	// Open the log file
	ol, err := os.OpenFile(*logDir+"/go-mysql-dump-to-gdrive.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer ol.Close()

	log.SetOutput(ol)

	// Set Filename
	if *allDatabase {
		log.Println("Dump " + hostname + " DBs to GDrive Start")
		filename = hostname + "_" + now + ".sql"
	} else {
		log.Println("Dump DB " + *mysqlDb + " to GDrive Start")
		filename = *mysqlDb + "_" + now + ".sql"
	}

	if *gzipEnable {
		filename += ".gzip"
		filetype = "application/x-gzip"
	}

	// Set Client Identity
	data, err := ioutil.ReadFile(*oAuthSecretFile)
	if err != nil {
		log.Fatal(err)
	}

	config, err := google.JWTConfigFromJSON(data, "https://www.googleapis.com/auth/drive")
	if err != nil {
		log.Fatal(err)
	}

	client := config.Client(oauth2.NoContext)

	svc, err := drive.New(client)
	if err != nil {
		log.Fatalf("An error occurred creating Drive client: %v\n", err)
	}

	// Define the metadata for the file we are going to create.
	f := &drive.File{
		Title:       filename,
		Description: filename,
		MimeType:    filetype,
	}

	// Define local tmp file
	localTmpFile := *tmpDir + "/" + filename

	// Compose mysqldump command
	mysqldumpCommand := "mysqldump -u " + *mysqlUser + " -h " + *mysqlHost + " "
	if *allDatabase {
		mysqldumpCommand += "--all-databases "
	} else if *mysqlDb != "" {
		mysqldumpCommand += *mysqlDb
	} else {
		log.Fatal("You must specify a DB Name")
	}

	// Create database dump and store it on local tmp file
	cmd := exec.Command("/bin/bash", "-c", mysqldumpCommand)
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		log.Fatal("Mysqldump Error", err)
	}

	// Create a gzip file of the dump output stream
	if *gzipEnable {
		var outGzip bytes.Buffer
		w := gzip.NewWriter(&outGzip)
		w.Write(out.Bytes())
		w.Close()

		out = outGzip
	}

	// Write the gzip stream to a tmp file
	ioutil.WriteFile(localTmpFile, out.Bytes(), 0666)

	// Read the file data that we are going to upload.
	m, err := os.Open(localTmpFile)
	if err != nil {
		log.Fatalf("An error occurred reading the document: %v\n", err)
	}

	// Make the API request to upload metadata and file data.
	r, err := svc.Files.Insert(f).Media(m).Do()
	if err != nil {
		log.Fatalf("An error occurred uploading the document: %v\n", err)
	}
	log.Printf("Created: ID=%v, Title=%v\n", r.Id, r.Title)

	// Delete local tmp file
	cmd = exec.Command("/bin/bash", "-c", "rm "+localTmpFile)
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	// Calc the backup expiration time
	deleteDateTime := time.Now().Add(-*backupDuration).Format(time.RFC3339)

	// Get List of remote backup files
	l, err := svc.Files.List().Q("modifiedDate < '" + deleteDateTime + "'").Do()
	if err != nil {
		log.Fatalf("An error occurred listing the files: %v\n", err)
	}

	// Delete all the backup expired files
	for _, file := range l.Items {
		log.Println("Deleting: ", file.Title)
		err = svc.Files.Delete(file.Id).Do()
		if err != nil {
			log.Fatalf("An error occurred deleting: %v\n", f.Title)
		}
	}

	log.Println("Dump DB " + *mysqlDb + " to Drive Finish")
}
