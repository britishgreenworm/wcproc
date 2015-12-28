package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
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
	Category  string
	ArticleId string
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

//articleId is the class or id name of the element in which to get the articles words from (include . or #)
type FeedSetting struct {
	Name      string
	URL       string
	ArticleId string
}

func main() {

	feedSettings := []FeedSetting{
		{Name: "CNN", URL: "http://rss.cnn.com/rss/cnn_topstories.rss", ArticleId: ".zn-body__paragraph"},
		{Name: "CBS", URL: "http://www.cbsnews.com/latest/rss/main", ArticleId: "#article-entry"},
		{Name: "BBC", URL: "http://feeds.bbci.co.uk/news/rss.xml", ArticleId: ".story-body__inner"}}

	//time inverval when feed starts, feeds to put in, (utf8 only)
	go startFeeder(300, feedSettings)
	go startWordProc(301)

	http.HandleFunc("/", handler)
	http.HandleFunc("/api/getwords", getWordHandler)
	http.ListenAndServe(":8080", nil)

}

func startFeeder(seconds int, feedSettings []FeedSetting) {
	for true {
		//only can use utf-8 encoded xml files
		for _, feed := range feedSettings {
			fmt.Printf("Looking for new feeds in... %v \n", feed.Name)
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

			processWords(feed)
			fmt.Printf("processed..: %v from: %v \n", feed.Title, feed.Category)

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
	//db.feeds.aggregate({$match: { category: "BBC"}}, { $project: {  Words: 1 }}, { $unwind: "$Words" }, { $group: { _id: "$Words.name", count: { $sum: 1 } }});

}

func getWordHandler(w http.ResponseWriter, r *http.Request) {

	matchParam := r.URL.Query()["category"][0]

	session, _ := mgo.Dial("localhost")
	wordsCollection := session.DB("wcproc").C("feeds")

	project := bson.M{"$project": bson.M{"Words": 1}}
	match := bson.M{"$match": bson.M{"category": matchParam}}
	unWind := bson.M{"$unwind": "$Words"}

	group := bson.M{"$group": bson.M{"_id": "$Words._id", "count": bson.M{"$sum": 1}}}

	sort := bson.M{"$sort": bson.M{"count": -1}}
	limit := bson.M{"$limit": 50}

	operations := []bson.M{match, project, unWind, group, sort, limit}

	pipe := wordsCollection.Pipe(operations)

	var results []Word
	err := pipe.All(&results)
	if err != nil {
		fmt.Printf("%v", err.Error())
	}

	session.Close()

	jsonWords, err := json.Marshal(results)
	fmt.Fprintf(w, string(jsonWords))

	//use to count words
	//db.feeds.aggregate({ $project: {  Words: 1 }}, { $unwind: "$Words" }, { $group: { _id: "$Words.name", count: { $sum: 1 } }});

}

func getFeeds(feedSetting FeedSetting) {

	resp, err := http.Get(feedSetting.URL)
	if err != nil {
		log.Fatal(err)
	}

	var results Result

	bytes, _ := ioutil.ReadAll(resp.Body)

	xml.Unmarshal([]byte(bytes), &results)

	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	feed := Feed{}
	feeds.Find(bson.M{"category": feedSetting.Name}).Sort("-date").One(&feed)

	//convert the feed.date string to time.Time since unmarshal wont do it for me
	for iter, element := range results.ItemList {

		lastThree := element.PubDate[len(element.PubDate)-3:]

		if _, err := strconv.Atoi(lastThree); err == nil {
			results.ItemList[iter].Date, err = time.Parse(time.RFC1123Z, element.PubDate)
			if err != nil {
				fmt.Println(err.Error())
			}
		} else {
			results.ItemList[iter].Date, err = time.Parse(time.RFC1123, element.PubDate)
			if err != nil {
				fmt.Println(err.Error())
			}
		}

	}

	//check to see if any articles exist past the last article date in db

	for iter, element := range results.ItemList {

		if element.Date.After(feed.Date.Local()) {
			fmt.Printf("adding:  %v -- %v \n", element.Date, element.Title)
			results.ItemList[iter].ArticleId = feedSetting.ArticleId
			results.ItemList[iter].Category = feedSetting.Name
			_ = feeds.Insert(results.ItemList[iter])
		}
	}

	session.Close()

}

//array functionality with strings.contains
func containWords(word string, words []string) bool {
	for _, ele := range words {
		if strings.Contains(strings.ToLower(strings.Trim(word, ". ,")), ele) {
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

	body := cascadia.MustCompile(feed.ArticleId).MatchAll(doc)

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

	omitWords := []string{"the", "of", "a", "at", "as", "with", "been", "in", "that", "and", "with", "from", "more", "been", "we", "not", "by", "he", "who", "were",
		"so", "just", "also", "his", "will", "up", "had", "out", "if", "an", "to", "on", "which", "just", "they", "is", "it", "but", "its", "could", "us",
		"him", "next", "time", "like", "...", "both", "stil", "why", "it", "even", "no", "do", "first", "two", "for", "or", "our", "did", "very", "yet",
		"most", "new", "how", "you", "i", "we", "sure", "move", "close", "until", "my", "get", "go", "those", "though", "be", " ", "me", "met", "recent",
		"rest", "end", "put", "seen", "else", "should", "met", "center", "over", "would", "much", "lot", "room", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "see", "set", "mr", "few", "old", "key", "sent", "tell", "ever", "under", "through", "led", "own", "such", "people",
		"due", "role", "never", "look", "full", "expected", "try"}

	for key, value := range words {
		//get rid of words that have these in them
		if !strings.ContainsAny(key, "-<>/_{}=;#&()*%$@1234567890\"") {
			if !containWords(key, omitWords) {

				item := Word{Name: strings.ToLower(strings.Trim(key, ". ,")), Count: value}
				feed.Words = append(feed.Words, item)
			}
		}
	}

	feed.Processed = true
	feeds.Update(bson.M{"_id": feed.Id}, feed)
	session.Close()

}
