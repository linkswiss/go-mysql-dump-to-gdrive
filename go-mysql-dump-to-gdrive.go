package main

import (
	"bytes"
	"code.google.com/p/google-api-go-client/drive/v2"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/net/context"
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
	oAuthCode       = flag.String("code", "", "Authorization Code")
	oAuthSecretFile = flag.String("secret-file", "client_secret.json", "Secret File (default client_secret.json)")
	oAuthCacheFile  = flag.String("cache-file", "cache_token.json", "Cache Token File (default cache_token.json)")
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

	config, err := google.ConfigFromJSON(data, "https://www.googleapis.com/auth/drive")
	if err != nil {
		log.Fatal(err)
	}

	// Check i f cache token exist
	if _, err := os.Stat(*oAuthCacheFile); os.IsNotExist(err) && *oAuthCode == "" {
		retrieveCodeUrl(config)
		return
	} else if os.IsNotExist(err) && *oAuthCode != "" {
		err := exchangeToken(config, *oAuthCode, *oAuthCacheFile)
		if err != nil {
			log.Fatal("Error in exchangeToken %v\n", err)
		}
	}

	// Generate a URL to visit for authorization.
	token_file, err := ioutil.ReadFile(*oAuthCacheFile)
	if err != nil {
		log.Fatal("Error reading Token Cache %v\n", err)
	}

	// Check Token stored on TokenCache
	var token oauth2.Token
	err = json.Unmarshal(token_file, &token)
	if err != nil {
		log.Fatal("Error converting Token Cache %v\n", err)
	}
	tok := &oauth2.Token{RefreshToken: token.RefreshToken}

	token_source := config.TokenSource(oauth2.NoContext, tok)

	cache, err := json.Marshal(tok)
	if err != nil {
		log.Fatal("JSON Marshal Token error %v\n", err)
	}
	ioutil.WriteFile(*oAuthCacheFile, []byte(cache), 0666)

	if !token.Valid() {
		if *oAuthCode == "" {
			retrieveCodeUrl(config)
			return
		}
		err := exchangeToken(config, *oAuthCode, *oAuthCacheFile)
		if err != nil {
			log.Fatal("Error in exchangeToken %v\n", err)
		}
	}

	svc, err := drive.New(oauth2.NewClient(oauth2.NoContext, token_source))
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

func retrieveCodeUrl(config *oauth2.Config) {
	// Get an authorization code from the data provider.
	// ("Please ask the user if I can access this resource.")
	url := config.AuthCodeURL("")
	fmt.Println("Visit this URL to get a code, then run again with -code=YOUR_CODE\n")
	fmt.Println(url)
}

func exchangeToken(config *oauth2.Config, code string, token_cache_file string) (err error) {
	// Exchange the authorization code for an access token.
	// ("Here's the code you gave the user, now give me a token!")
	token_source, err := config.Exchange(context.TODO(), code)
	if err != nil {
		log.Fatal("Config Exchange %v\n", err)
	}
	cache, err := json.Marshal(token_source)
	if err != nil {
		log.Fatal("JSON Marshal Token error %v\n", err)
	}
	ioutil.WriteFile(token_cache_file, []byte(cache), 0666)
	fmt.Printf("Token is cached", token_source)

	return err
}
