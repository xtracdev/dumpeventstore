package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"golang.org/x/tools/blog/atom"
)

//HttpFeedReader defines a type for an Http Feed reader
type HttpFeedReader struct {
	endpoint string
	client   *http.Client
	proto    string
	keyAlias string
	kmsSvc   *kms.KMS
}

//NewHttpFeedReader is a factory for instantiating HttpFeedReaders
func NewHttpFeedReader(endpoint, feedProto, keyAlias string, kmsSvc *kms.KMS) *HttpFeedReader {

	client := http.DefaultClient
	if feedProto == "https" {
		tr := http.DefaultTransport
		defTransAsTransPort := tr.(*http.Transport)
		defTransAsTransPort.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client = &http.Client{Transport: tr}
	}

	return &HttpFeedReader{
		endpoint: endpoint,
		client:   client,
		proto:    feedProto,
		keyAlias: keyAlias,
		kmsSvc:   kmsSvc,
	}
}

//GetRecent returns the recent notifications
func (hr *HttpFeedReader) GetRecent() (*atom.Feed, error) {
	url := fmt.Sprintf("%s://%s/notifications/recent", hr.proto, hr.endpoint)
	return hr.getResource(url)
}

//GetFeed returns the specific feed
func (hr *HttpFeedReader) GetFeed(feedid string) (*atom.Feed, error) {
	url := fmt.Sprintf("%s://%s/notifications/%s", hr.proto, hr.endpoint, feedid)
	return hr.getResource(url)
}

//IsFeedEncrypted indicates if we use a key alias for decrypting the feed
func (hr *HttpFeedReader) IsFeedEncrypted() bool {
	return hr.keyAlias != ""
}

//Decrypt from cryptopasta commit bc3a108a5776376aa811eea34b93383837994340
//used via the CC0 license. See https://github.com/gtank/cryptopasta
func (hr *HttpFeedReader) decrypt(ciphertext []byte, key *[32]byte) (plaintext []byte, err error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("malformed ciphertext")
	}

	return gcm.Open(nil,
		ciphertext[:gcm.NonceSize()],
		ciphertext[gcm.NonceSize():],
		nil,
	)
}

//DecryptFeed uses the AWS KMS to decrypt the feed text.
func (hr *HttpFeedReader) DecryptFeed(feedBytes []byte) ([]byte, error) {
	//Message is encrypted encryption key + :: + encrypted message
	parts := strings.Split(string(feedBytes), "::")
	if len(parts) != 2 {
		err := errors.New(fmt.Sprintf("Expected two parts, got %d", len(parts)))
		return nil, err
	}

	//Decode the key and the text
	keyBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}

	//Get the encrypted bytes
	msgBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	//Decrypt the encryption key
	di := &kms.DecryptInput{
		CiphertextBlob: keyBytes,
	}

	decryptedKey, err := hr.kmsSvc.Decrypt(di)
	if err != nil {
		return nil, err
	}

	//Use the decrypted key to decrypt the message text
	decryptKey := [32]byte{}

	copy(decryptKey[:], decryptedKey.Plaintext[0:32])

	return hr.decrypt(msgBytes, &decryptKey)
}

//getResource does a git on the specified feed resource
func (hr *HttpFeedReader) getResource(url string) (*atom.Feed, error) {

	log.Infof("Get %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := hr.client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBytes, readErr := ioutil.ReadAll(resp.Body)
		if readErr == nil {
			log.Warnf("Error reading feed: %d %s", resp.StatusCode, string([]byte(responseBytes)))
		}
		return nil, errors.New(fmt.Sprintf("Error retrieving resource: %d", resp.StatusCode))
	}

	responseBytes, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	//Are we using a key to decrypt the feed?
	if hr.IsFeedEncrypted() {
		responseBytes, err = hr.DecryptFeed(responseBytes)
		if err != nil {
			return nil, err
		}
	}

	var feed atom.Feed

	err = xml.Unmarshal(responseBytes, &feed)

	if err != nil {
		return nil, err
	}

	return &feed, nil
}

func createFeedReader() (*HttpFeedReader, error) {
	feedAddr := os.Getenv("ATOMFEED_ENDPOINT")
	if feedAddr == "" {
		return nil, errors.New("Missing ATOMFEED_ENDPOINT environment variable value")
	}

	var kmsService *kms.KMS
	keyAlias := os.Getenv("KEY_ALIAS")
	if keyAlias != "" {
		log.Info("Configuration indicates use of KMS with alias ", keyAlias)

		sess, err := session.NewSession()
		if err != nil {
			log.Errorf("Unable to establish AWS session: %s. Exiting.", err.Error())
			os.Exit(1)
		}
		kmsService = kms.New(sess)
	}

	proto := os.Getenv("FEED_PROTO")
	if proto == "" {
		log.Info("Defaulting feed proto to https")
		proto = "https"
	}

	return NewHttpFeedReader(feedAddr, proto, keyAlias, kmsService), nil
}

//Get link extracts the given link relationship from the given feed's
//link collection
func getLink(linkRelationship string, feed *atom.Feed) *string {
	if feed == nil {
		return nil
	}

	for _, l := range feed.Link {
		if l.Rel == linkRelationship {
			return &l.Href
		}
	}

	return nil
}

//Grab the feed id as the component of a uri
func feedIdFromResource(feedURL string) string {
	url, _ := url.Parse(feedURL)
	parts := strings.Split(url.RequestURI(), "/")
	return parts[len(parts)-1]
}

//Get first feed navigates a feed set from the recent feed all the way back
//to the first acchived feed
func getFirstFeed(feedReader *HttpFeedReader) (*atom.Feed, error) {
	log.Info("Looking for first feed")
	//Start with recent
	var feed *atom.Feed
	var feedReadError error

	feed, feedReadError = feedReader.GetRecent()
	if feedReadError != nil {
		return nil, feedReadError
	}

	if feed == nil {
		//Nothing in the feed if there's no recent available...
		log.Info("Nothing in the feed")
		return nil, nil
	}

	log.Info("Got feed - navigate prev-archive link")
	for {
		prev := getLink("prev-archive", feed)
		if prev == nil {
			break
		}

		//Extract feed id from prev
		feedID := feedIdFromResource(*prev)
		log.Infof("Prev archive feed id is %s", feedID)
		feed, feedReadError = feedReader.GetFeed(feedID)
		if feedReadError != nil {
			return nil, feedReadError
		}
	}

	return feed, nil
}

func main() {
	feedReader, err := createFeedReader()
	if err != nil {
		log.Fatalf("Error creating feed reader: %s", err.Error())
	}

	first, err := getFirstFeed(feedReader)
	if err != nil {
		log.Fatalf("Read: %s", err.Error())
	}

	log.Info(first.Link)
	for _, entry := range first.Entry {
		fmt.Printf("%s %s\n", entry.ID, entry.Content.Body)
	}

	feed := first
	var feedReadError error
	for {
		next := getLink("next-archive", feed)
		if next == nil {
			break
		}

		//Extract feed id from prev
		feedID := feedIdFromResource(*next)
		log.Infof("Next archive feed id is %s", feedID)
		feed, feedReadError = feedReader.GetFeed(feedID)
		if feedReadError != nil {
			log.Fatal(feedReadError.Error())
		}

		for _, entry := range feed.Entry {
			fmt.Printf("%s %s %s %s\n", entry.ID, entry.Content.Body, entry.Published, entry.Content.Type)
		}
	}

}
