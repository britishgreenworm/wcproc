package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/andybalholm/cascadia"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

//Word Contains Marshalled Mongo document data: parsed words of a feed
type Word struct {
	Name  string `bson:"_id" json:"name"`
	Count int    `json:"count"`
}

type Grouping struct {
	Date     string `bson:"date" json:"date"`
	Category string `bson:"category" json:"category"`
}

type jsonWordFreq struct {
	Groupings Grouping `bson:"_id" json:"grouping"`
	Count     int      `json:"count"`
}

//Feed Contains Marshalled Mongo document data: parent feed data
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

//FeedCount container for feed count
type FeedCount struct {
	Id    bson.ObjectId `bson:"_id"`
	count string        `bson:"count"`
}

//Result For storing rss xml Marshalled data
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

//Page stores html response
type Page struct {
	Title string
	Body  []byte
}

//FeedSetting contains: articleId is the class or id name of the element in which to get the articles words from (include . or #)
type FeedSetting struct {
	Name      string
	URL       string
	ArticleId string
}

func main() {

	feedSettings := []FeedSetting{
		{Name: "CNN", URL: "http://rss.cnn.com/rss/cnn_topstories.rss", ArticleId: ".zn-body__paragraph"},
		{Name: "CBS", URL: "http://www.cbsnews.com/latest/rss/main", ArticleId: "#article-entry"},
		{Name: "BBC", URL: "http://feeds.bbci.co.uk/news/rss.xml", ArticleId: ".story-body__inner"},
		{Name: "FOX", URL: "http://feeds.foxnews.com/foxnews/latest?format=xml", ArticleId: ".article-text"},
		{Name: "NPR", URL: "http://www.npr.org/rss/rss.php?id=1001", ArticleId: "#storytext"}}

	//time inverval when check for new feeds
	go startFeeder(300, feedSettings)
	go startWordProc(301)

	http.HandleFunc("/", handler)
	http.HandleFunc("/api/getwords", getWordHandler)
	http.HandleFunc("/api/getArticleCount", getArticleCount)
	http.HandleFunc("/api/getTimeLine", getTimeLine)
	http.ListenAndServe(":8080", nil)

}

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
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

}

func getArticleCount(w http.ResponseWriter, r *http.Request) {

	//categoryParam := r.URL.Query()["category"][0]

	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	//match := bson.M{"$match": bson.M{"category": "CBS"}}
	group := bson.M{"$group": bson.M{"_id": 1, "count": bson.M{"$sum": 1}}}
	//group := bson.M{"$group": bson.M{"_id": "$Words._id", "count": bson.M{"$sum": bson.M{"$add": "$Words.count"}}}}
	operations := []bson.M{group}
	pipe := feeds.Pipe(operations)

	var results FeedCount
	err := pipe.One(&results)
	checkError(err)

	session.Close()

	jsonWords, err := json.Marshal(results)
	if err != nil {
		fmt.Printf("%v", err.Error())
	}

	fmt.Fprintf(w, string(jsonWords))
}

func getTimeLine(w http.ResponseWriter, r *http.Request) {
	//db.feeds.aggregate({$project: {dater: { $dateToString: { format: "%Y-%m-%d", date: "$date" }}, Words:1, category:1}}, {$unwind: "$Words"}, {$match: {$and: [{ category: "FOX"}, {"Words._id" : "trump" }]}}, {$group : {_id: "$dater", count: { $sum: {$add: "$Words.count"}}}}, {$sort: {dater: -1}})

	wordParam := strings.ToLower(r.URL.Query()["word"][0])

	fmt.Printf("Querying Timeline for: %v \n", wordParam)

	session, _ := mgo.Dial("localhost")
	wordsCollection := session.DB("wcproc").C("feeds")

	project := bson.M{"$project": bson.M{"dater": bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$date"}}, "Words": 1, "category": 1}}
	unWind := bson.M{"$unwind": "$Words"}
	match := bson.M{"$match": bson.M{"Words._id": wordParam}}
	group := bson.M{"$group": bson.M{"_id": bson.M{"date": "$dater", "category": "$category"}, "count": bson.M{"$sum": bson.M{"$add": "$Words.count"}}}}
	sort := bson.M{"$sort": bson.M{"_id": 1}}

	operations := []bson.M{project, unWind, match, group, sort}
	pipe := wordsCollection.Pipe(operations)
	var results []jsonWordFreq
	err := pipe.All(&results)
	checkError(err)

	session.Close()

	jsonWords, err := json.Marshal(results)
	checkError(err)
	fmt.Fprintf(w, string(jsonWords))

}

func getWordHandler(w http.ResponseWriter, r *http.Request) {

	//mongo query used below
	//db.feeds.aggregate({$match: { category: "BBC"}}, { $project: {  Words: 1 }}, { $unwind: "$Words" }, { $group: { _id: "$Words._id", count: { $sum: {$add: "$Words.count"} } }});

	//this gets a specific word and category for each date

	matchParam := r.URL.Query()["category"][0]
	filterParam := r.URL.Query()["filter"][0]
	filterParamAry := strings.Split(filterParam, ",")

	fmt.Printf("Querying Wordhandler for: %v \n", filterParam)

	session, _ := mgo.Dial("localhost")
	wordsCollection := session.DB("wcproc").C("feeds")

	project := bson.M{"$project": bson.M{"Words": 1}}
	match := bson.M{"$match": bson.M{"category": matchParam}}
	unWind := bson.M{"$unwind": "$Words"}

	group := bson.M{"$group": bson.M{"_id": "$Words._id", "count": bson.M{"$sum": bson.M{"$add": "$Words.count"}}}}

	sort := bson.M{"$sort": bson.M{"count": -1}}
	limit := bson.M{"$limit": 50}

	operations := []bson.M{}

	if len(filterParamAry[0]) != 0 {

		tempAry := []interface{}{}

		for _, i := range filterParamAry {
			tempAry = append(tempAry, bson.M{"_id": strings.Trim(i, " ")})
		}

		filter := bson.M{"$match": bson.M{"$or": tempAry}}

		operations = []bson.M{match, project, unWind, group, filter, sort, limit}
	} else {
		operations = []bson.M{match, project, unWind, group, sort, limit}
	}

	pipe := wordsCollection.Pipe(operations)

	var results []Word
	err := pipe.All(&results)
	checkError(err)

	session.Close()

	jsonWords, err := json.Marshal(results)
	checkError(err)
	fmt.Fprintf(w, string(jsonWords))
}

func getFeeds(feedSetting FeedSetting) {

	resp, err := http.Get(feedSetting.URL)

	//there's no reason to panic on this
	if err != nil {
		fmt.Printf("Couldn't reach URL: %v \n\n", feedSetting.URL)
		return
	}

	var results Result

	decoder := xml.NewDecoder(resp.Body)
	decoder.CharsetReader = charset.NewReaderLabel
	err = decoder.Decode(&results)
	checkError(err)

	//xml.Unmarshal([]byte(tempStr), &results)

	session, _ := mgo.Dial("localhost")
	feeds := session.DB("wcproc").C("feeds")

	feed := Feed{}
	feeds.Find(bson.M{"category": feedSetting.Name}).Sort("-date").One(&feed)

	//convert the feed.date string to time.Time since unmarshal wont do it for me
	for iter, element := range results.ItemList {

		lastThree := element.PubDate[len(element.PubDate)-3:]
		results.ItemList[iter].Link = strings.Trim(element.Link, " ")

		if _, err := strconv.Atoi(lastThree); err == nil {
			results.ItemList[iter].Date, err = time.Parse(time.RFC1123Z, element.PubDate)
			checkError(err)
		} else {
			results.ItemList[iter].Date, err = time.Parse(time.RFC1123, element.PubDate)
			checkError(err)
		}

	}

	//check to see if any articles exist past the last article date in db
	for iter, element := range results.ItemList {

		if element.Date.After(feed.Date.Local()) {

			//check title name against db for duplicates, some news feeds like to change the pubdate over the length of 6 hours
			matchup := Feed{}
			feeds.Find(bson.M{"title": element.Title, "category": feedSetting.Name}).One(&matchup)

			if matchup.Category == "" {
				fmt.Printf("adding:  %v -- %v \n", element.Date, element.Title)
				results.ItemList[iter].ArticleId = feedSetting.ArticleId
				results.ItemList[iter].Category = feedSetting.Name
				_ = feeds.Insert(results.ItemList[iter])
			}
		}
	}
	session.Close()
}

//array functionality with strings.contains
func containWords(word string, words []string) bool {
	for _, ele := range words {
		if strings.Compare(strings.ToLower(strings.Trim(word, ". ,")), ele) == 0 {
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
	//there's no reason to panic on this
	if err != nil {
		fmt.Printf("Couldn't reach URL: %v \n\n", feed.Link)
		return
	}

	doc, err := html.Parse(resp.Body)

	checkError(err)

	body := cascadia.MustCompile(feed.ArticleId).MatchAll(doc)

	var strBuffer bytes.Buffer
	re := regexp.MustCompile("\\<[^>]*\\>")

	for _, element := range body {
		var buf bytes.Buffer
		html.Render(&buf, element)

		strBuffer.WriteString(" " + re.ReplaceAllString(html.UnescapeString(buf.String()), ""))
		//fmt.Printf("... %v ... \n", re.ReplaceAllString(html.UnescapeString(buf.String()), ""))
	}

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
		"most", "new", "how", "you", "i", "we", "sure", "move", "close", "until", "my", "get", "go", "those", "though", "be", "me", "met", "recent",
		"rest", "end", "put", "seen", "else", "should", "met", "center", "over", "would", "much", "lot", "room", "three", "four", "five", "six", "seven",
		"eight", "nine", "ten", "see", "set", "mr", "few", "old", "key", "sent", "tell", "ever", "under", "through", "led", "own", "such", "people",
		"due", "role", "never", "look", "full", "try", "was", "said", "this", "are", "their", "when", "can", "now", "after", "than", "some", "when",
		"her", "image", "about", "she", "i", "all", "one", "have", "has", "your", "what", "other", "there", "caption", "copyright"}

	//fmt.Printf("OMITTING:")
	for key, value := range words {
		//get rid of words that have these in them
		if !strings.ContainsAny(key, "-<>/_{}=;#&()*%$@1234567890") {
			if !containWords(key, omitWords) {

				//keep these words but trim off these chars
				item := Word{Name: strings.ToLower(strings.Trim(key, ". ,\"")), Count: value}
				feed.Words = append(feed.Words, item)
			} else {
				//fmt.Printf("%v \n", key)
			}
		} else {
			//fmt.Printf("%v \n", key)
		}
	}

	feed.Processed = true
	feeds.Update(bson.M{"_id": feed.Id}, feed)
	session.Close()

}
