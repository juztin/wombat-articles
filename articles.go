package articles

import (
	"fmt"
	"log"
	"strings"
	"time"

	"bitbucket.org/juztin/wombat/backends"
)

type Img struct {
	Src string `src` //`json:"src"` // data:"src"`
	Alt string `alt` //`json:"alt"` // data:"alt"`
	W   int    `w`   //`json:"w"`   // data:"w"`
	H   int    `h`   //`json:"h"`   // data:"h"`
}

type Article struct {
	Printer     `-`       //`json:"-"`
	TitlePath   string    `titlePath`   //`json:"titlePath"` // data:"titlepath"`
	Title       string    `title`       //`json:"title"`     // data:"title"`
	Synopsis    string    `synopsis`    //`json:"synopsis"   // data:"synopsis"`
	Content     string    `content`     //`json:"content"`   // data:"content"`
	IsPublished bool      `isPublished` //`json:"isActive"`  // data:"isActive"`
	Created     time.Time `created`     //`json:"created"`   // data:"created"`
	Modified    time.Time `modified`    //`json:"modified"`  // data:"modified"`
	Img         Img       `img`         //`json:"img"`       // data:"img"`
	Imgs        []Img     `imgs`        //`json:"imgs"`      // data:"imgs"`
}

type Articles struct {
	Reader
}

type Reader interface {
	ByTitlePath(titlePath string, unPublished bool) (interface{}, error)
	Recent(limit, page int, unPublished bool) (interface{}, error)
}

type Printer interface {
	Print(article interface{}) error
	UpdateSynopsis(titlePath, synopsis string, modified time.Time) error
	UpdateContent(titlePath, content string, modified time.Time) error
	Delete(titlePath string) error
	Publish(titlePath string, publish bool) error
	WriteImg(titlePath string, img interface{}) error
	WriteImgs(titlePath string, imgs interface{}) error
}

const VERSION string = "0.0.1"

func New() Articles {
	var r Reader
	if p, err := backends.Open("wombat:apps:article-reader"); err != nil {
		log.Fatal("No 'article' reader available")
	} else {
		if o, ok := p.(Reader); !ok {
			log.Fatal("Invalid 'article' reader")
		} else {
			r = o
		}
	}
	return Articles{r}
}

func NewArticle(title string) *Article {
	if p, err := backends.Open("wombat:apps:article-printer"); err != nil {
		log.Println("No 'article' printer available")
	} else {
		if printer, ok := p.(Printer); !ok {
			log.Println("Invalid 'article' printer")
		} else {
			tp, t := titlePathTime(title)
			return &Article{Printer: printer,
				Title:     title,
				TitlePath: tp,
				Created:   t}
		}
	}
	return nil
}

func titlePathTime(title string) (string, time.Time) {
	// create a new article, based on the current time
	t := time.Now()
	titlePath := fmt.Sprintf("%d/%02d/%02d/%s/",
		t.Year(),
		t.Month(),
		t.Day(),
		strings.Replace(title, " ", "-", -1))
	return titlePath, t
}

func (a *Article) Print() error {
	return a.Printer.Print(a)
}
func (a *Article) UpdateContent(content string) error {
	return a.Printer.UpdateContent(a.TitlePath, content, time.Now())
}

func (a *Article) SetSynopsis(synopsis string) (err error) {
	modified := time.Now()
	if err = a.Printer.UpdateSynopsis(a.TitlePath, synopsis, modified); err == nil {
		a.Synopsis = synopsis
		a.Modified = modified
	}
	return
}

func (a *Article) SetContent(content string) (err error) {
	modified := time.Now()
	if err = a.Printer.UpdateContent(a.TitlePath, content, modified); err == nil {
		a.Content = content
		a.Modified = modified
	}
	return
}

func (a *Article) Delete() error {
	return a.Printer.Delete(a.TitlePath)
}

func (a *Article) Publish(publish bool) (err error) {
	if err = a.Printer.Publish(a.TitlePath, publish); err == nil {
		a.IsPublished = publish
	}
	return
}

func (a *Article) SetImg(img Img) (err error) {
	if err = a.Printer.WriteImg(a.TitlePath, img); err == nil {
		a.Img = img
	}
	return
}

func (a *Article) SetImgs(imgs []Img) (err error) {
	if err = a.Printer.WriteImgs(a.TitlePath, imgs); err == nil {
		a.Imgs = imgs
	}
	return
}
