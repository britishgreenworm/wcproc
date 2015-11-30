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
	"golang.org/x/net/html"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type Word struct {
	Name  string `bson:"_id" json:"name"`
	Count int    `json:"count"`
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

type Wrapper struct {
	Words []Word
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

type Page struct {
	Title string
	Body  []byte
}

func main() {

	feeds := []string{"http://rss.cnn.com/rss/cnn_topstories.rss"}

	//time inverval when feed starts, feeds to put in, (utf8 only)
	go startFeeder(10, feeds)
	go startWordProc(11)

	http.HandleFunc("/", handler)
	http.HandleFunc("/api/getwords", getWordHandler)
	http.ListenAndServe(":8080", nil)

}

func startFeeder(seconds int, feeds []string) {
	for true {
		//only can use utf-8 encoded xml files
		for _, feed := range feeds {
			getFeeds(feed)
		}

		time.Sleep(time.Duration(seconds) * time.Second)
	}
}

func startWordProc(seconds int) {
	for true {
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
		time.Sleep(time.Duration(seconds) * time.Millisecond)
	}
}

func loadPage(title string) []byte {
	filename := title
	body, _ := ioutil.ReadFile(filename)
	fmt.Printf("loading:  %v \n", title)

	return body
}

func handler(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Path[1:]
	p := loadPage(title)

	fmt.Fprintf(w, string(p))

	//use to count words
	//db.feeds.aggregate({ $project: {  Words: 1 }}, { $unwind: "$Words" }, { $group: { _id: "$Words.name", count: { $sum: 1 } }});

}

func getWordHandler(w http.ResponseWriter, r *http.Request) {

	session, _ := mgo.Dial("localhost")
	wordsCollection := session.DB("wcproc").C("feeds")

	project := bson.M{"$project": bson.M{"Words": 1}}
	unWind := bson.M{"$unwind": "$Words"}

	group := bson.M{"$group": bson.M{"_id": "$Words.name", "count": bson.M{"$sum": 1}}}

	sort := bson.M{"$sort": bson.M{"count": -1}}
	//limit := bson.M{"$limit": 100}

	operations := []bson.M{project, unWind, group, sort}

	pipe := wordsCollection.Pipe(operations)

	var results []Word
	err := pipe.All(&results)
	if err != nil {
		fmt.Printf("%v", err.Error())
	}

	session.Close()

	for _, word := range results {
		fmt.Printf("name: %v %v \n", word.Name, word.Count)
		fmt.Fprintf(w, "name: %v %v \n", word.Name, word.Count)
	}

	//use to count words
	//db.feeds.aggregate({ $project: {  Words: 1 }}, { $unwind: "$Words" }, { $group: { _id: "$Words.name", count: { $sum: 1 } }});

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

//array functionality with strings.contains
func containWords(word string, words []string) bool {
	for _, ele := range words {
		if !strings.Contains(word, ele) {
			return true
		}
	}

	return false
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
