package main

import (
	"bytes"
	"code.google.com/p/goauth2/oauth"
	"code.google.com/p/google-api-go-client/drive/v2"
	"compress/gzip"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// Settings for authorization.
var config = &oauth.Config{
	Scope:       "https://www.googleapis.com/auth/drive",
	RedirectURL: "urn:ietf:wg:oauth:2.0:oob",
	AuthURL:     "https://accounts.google.com/o/oauth2/auth",
	TokenURL:    "https://accounts.google.com/o/oauth2/token",
}

// Set the command line arguments
var (
	oAuthClientId     = flag.String("client-id", "", "Google API OAuth Client ID")
	oAuthClientSecret = flag.String("client-secret", "", "Google API OAuth Client Secret")
	oAuthCode         = flag.String("code", "", "Authorization Code")
	oAuthCacheFile    = flag.String("cache-file", "cache.json", "Cache File Name (default cache.json)")
	gDriveFolderId    = flag.String("gdrive-folder-id", "", "Google Drive Backup Folder ID")
	mysqlUser         = flag.String("db-user", "", "Name of your MySql dump USER")
	mysqlHost         = flag.String("db-host", "localhost", "Name of your MySql dump HOST")
	mysqlDb           = flag.String("db", "", "Database Name")
	allDatabase       = flag.Bool("dump-all", false, "If set script dump all MySql Databases")
	backupDuration    = flag.Duration("keep-last", 168*time.Hour, "time.Duration (Keep last backups i.e. 10m, 24h (default 168h, last 7 days))")
	tmpDir            = flag.String("tmp-dir", "/tmp", "Temp directory (default /tmp)")
	logDir            = flag.String("log-dir", "/var/log", "Log directory (default /var/log)")
	gzipEnable        = flag.Bool("gzip", false, "If set Gzip Compression Enabled")
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
	config.ClientId = *oAuthClientId
	config.ClientSecret = *oAuthClientSecret

	// Get Cache file
	config.TokenCache = oauth.CacheFile(*oAuthCacheFile)

	// Generate a URL to visit for authorization.
	t := &oauth.Transport{
		Config:    config,
		Transport: http.DefaultTransport,
	}

	// Check Token stored on TokenCache
	token, err := config.TokenCache.Token()
	if err != nil {
		if *oAuthCode == "" {
			// Get an authorization code from the data provider.
			// ("Please ask the user if I can access this resource.")
			url := config.AuthCodeURL("")
			fmt.Println("Visit this URL to get a code, then run again with -code=YOUR_CODE\n")
			fmt.Println(url)
			return
		}
		// Exchange the authorization code for an access token.
		// ("Here's the code you gave the user, now give me a token!")
		token, err = t.Exchange(*oAuthCode)
		if err != nil {
			log.Fatal("Exchange:", err)
		}
		// (The Exchange method will automatically cache the token.)
		fmt.Printf("Token is cached in %v\n", config.TokenCache)
	}

	// Assign token to oauth Transport
	t.Token = token

	// Create a new authorized Drive client.
	svc, err := drive.New(t.Client())
	if err != nil {
		log.Fatalf("An error occurred creating Drive client: %v\n", err)
	}

	// Define the metadata for the file we are going to create.
	f := &drive.File{
		Title:       filename,
		Description: filename,
		MimeType:    filetype,
	}

	// Define the Backup Folder
	p := &drive.ParentReference{Id: *gDriveFolderId}

	// Set the Backup Folder to the file parent
	f.Parents = []*drive.ParentReference{p}

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
	l, err := svc.Files.List().Q("'" + *gDriveFolderId + "' in parents and modifiedDate < '" + deleteDateTime + "'").Do()
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
