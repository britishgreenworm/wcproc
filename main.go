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
	"gopkg.in/mgo.v2/bson"

	"golang.org/x/net/html"
)

type Word struct {
	Name  string `bson:"name"`
	Count int    `bson:"count"`
}

type Feed struct {
	Id        bson.ObjectId `bson:"_id,omitempty"`
	Title     string        `xml:"title"`
	Link      string        `xml:"link"`
	PubDate   string        `xml:"pubDate"`
	Date      time.Time
	Words     []Word `bson:"Words"`
	Processed bool
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

func main() {

	//only can use utf-8 encoded xml files
	getFeeds("http://rss.cnn.com/rss/cnn_topstories.rss")

	//grab all the feeds that haven't been processed
	session, _ := mgo.Dial("localhost")
	feedCollection := session.DB("wcproc").C("feeds")
	feeds := []Feed{}
	feedCollection.Find(bson.M{"processed": false}).All(&feeds)

	session.Close()

	//work through all the sites
	for _, feed := range feeds {

		fmt.Printf("processing..: %v \n", feed.Title)

		processWords(feed)
	}

}

func getFeeds(urlPath string) {

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
	feeds.Find(nil).Sort("-date").One(&feed)

	//convert the feed.date string to time.Time since unmarshal wont do it for me
	for iter, element := range results.ItemList {
		results.ItemList[iter].Date, _ = http.ParseTime(element.PubDate)
	}

	//check to see if any articles exist past the last article date in db
	fmt.Printf("feedDate: %v \n", feed.Date)

	for iter, element := range results.ItemList {
		//fmt.Printf("dbDates: %v \n", element.Date.Local())

		if element.Date.After(feed.Date.Local()) {
			fmt.Printf("adding:  %v -- %v \n", element.Date, element.Title)

			_ = feeds.Insert(results.ItemList[iter])
		}
	}

	session.Close()

}

func processWords(feed Feed) {

	//placeholder
	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

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
		return !unicode.IsLetter(c) && unicode.IsNumber(c)
	}
	strings.FieldsFunc(strBuffer.String(), f)

	words := make(map[string]int)

	for _, w := range strings.Fields(strBuffer.String()) {
		words[w]++
	}

	for key, value := range words {
		if !strings.ContainsAny(key, "<>/_=;#&()*%$@") {
			item := Word{Name: key, Count: value}
			feed.Words = append(feed.Words, item)
		}
	}

	feed.Processed = true
	feeds.Update(bson.M{"_id": feed.Id}, feed)
	session.Close()

}
