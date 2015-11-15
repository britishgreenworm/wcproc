package main

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"
	//"database/sql"
	"io/ioutil"
	//"github.com/go-sql-driver/mysql"
	//"github.com/PuerkitoBio/goquery"
	"encoding/xml"
	"log"

	"gopkg.in/mgo.v2"
)

func main() {

	getFeeds("http://rss.cnn.com/rss/cnn_topstories.rss")

}

func getFeeds(urlPath string) {
	type Feed struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		PubDate string `xml:"pubDate"`
	}

	type Result struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		// Required
		Title       string `xml:"channel>title"`
		Link        string `xml:"channel>link"`
		Description string `xml:"channel>description"`
		// Optional
		PubDate  string `xml:"channel>pubDate"`
		ItemList []Feed `xml:"channel>item"`
	}

	resp, err := http.Get(urlPath)
	if err != nil {
		log.Fatal(err)
	}

	var results Result
	//decoder := xml.NewDecoder(resp.Body)
	//decoder.CharsetReader = charset.NewReaderLabel
	//err2 := decoder.Decode(&results)
	bytes, _ := ioutil.ReadAll(resp.Body)

	xml.Unmarshal([]byte(bytes), &results)

	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	_ = feeds.Insert(results.ItemList)
	session.Close()

}

func processWords() {

	f := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	}
	fmt.Printf("Fields are: %q", strings.FieldsFunc("  foo1;bar2,baz3.  ttt  s ss ss..", f))

}
