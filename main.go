package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/andybalholm/cascadia"

	"gopkg.in/mgo.v2"

	"golang.org/x/net/html"
)

func main() {

	//getFeeds("http://rss.cnn.com/rss/cnn_topstories.rss")
	processWords()
}

func getFeeds(urlPath string) {
	type Feed struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		PubDate string `xml:"pubDate"`
		Date    time.Time
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

	bytes, _ := ioutil.ReadAll(resp.Body)

	xml.Unmarshal([]byte(bytes), &results)

	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	feed := Feed{}
	feeds.Find(nil).Sort("date : -1").One(&feed)

	//convert the feed.date string to time.Time since unmarshal wont do it for me
	for iter, element := range results.ItemList {
		results.ItemList[iter].Date, _ = http.ParseTime(element.PubDate)
	}

	//check to see if any articles exist past the last article date in db
	for iter, element := range results.ItemList {
		fmt.Printf("elementDate: %v,  feedDate: %v \n", element.Date, feed.Date)
		if element.Date.After(feed.Date) {
			_ = feeds.Insert(results.ItemList[iter])
		}
	}

	session.Close()

}

func processWords() {
	type Feed struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		PubDate string `xml:"pubDate"`
		Date    time.Time
	}

	type Word struct {
		name  string
		count int
	}

	//placeholder
	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	feed := Feed{}
	feeds.Find(nil).Sort("date : -1").One(&feed)
	session.Close()

	resp, err := http.Get(feed.Link)
	if err != nil {
		fmt.Println(err)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	body := cascadia.MustCompile(".zn-body__paragraph").MatchAll(doc)

	var strBuffer bytes.Buffer

	for _, element := range body {
		var buf bytes.Buffer
		html.Render(&buf, element)
		strBuffer.WriteString(" " + buf.String())
	}

	//----------------

	f := func(c rune) bool {
		return !unicode.IsLetter(c)
	}
	fmt.Printf("Fields are: %q", strings.FieldsFunc(strBuffer.String(), f))

	//processedStr := strings.FieldsFunc(strBuffer.String(), f)

	words := make(map[string]int)

	for _, w := range strings.Fields(strBuffer.String()) {
		words[w]++
	}

	for key, value := range words {
		fmt.Println("Key:", key, "Value: ", value, "\n")
	}

}
