package chapters

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

type Chapter struct {
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

type Chapters struct {
	Reader
}

type Reader interface {
	ByTitlePath(titlePath string, unPublished bool) (interface{}, error)
	Recent(limit, page int, unPublished bool) (interface{}, error)
}

type Printer interface {
	Print(chapter interface{}) error
	UpdateSynopsis(titlePath, synopsis string, modified time.Time) error
	UpdateContent(titlePath, content string, modified time.Time) error
	Delete(titlePath string) error
	Publish(titlePath string, publish bool) error
	WriteImg(titlePath string, img interface{}) error
	WriteImgs(titlePath string, imgs interface{}) error
}

func New() Chapters {
	var r Reader
	if p, err := backends.Open("mongo:apps:chapter-reader"); err != nil {
		log.Fatal("No 'chapter' reader available")
	} else {
		if o, ok := p.(Reader); !ok {
			log.Fatal("Invalid 'chapter' reader")
		} else {
			r = o
		}
	}
	return Chapters{r}
}

func NewChapter(title string) *Chapter {
	if p, err := backends.Open("mongo:apps:chapter-printer"); err != nil {
		log.Println("No 'chapter' printer available")
	} else {
		if printer, ok := p.(Printer); !ok {
			log.Println("Invalid 'chapter' printer")
		} else {
			tp, t := titlePathTime(title)
			return &Chapter{Printer: printer,
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
	titlePath := fmt.Sprintf("/%d/%02d/%02d/%s/",
		t.Year(),
		t.Month(),
		t.Day(),
		strings.Replace(title, " ", "-", -1))
	return titlePath, t
}

func (c *Chapter) Print() error {
	return c.Printer.Print(c)
}
func (c *Chapter) UpdateContent(content string) error {
	return c.Printer.UpdateContent(c.TitlePath, content, time.Now())
}

func (c *Chapter) SetSynopsis(synopsis string) error {
	modified := time.Now()
	if err := c.Printer.UpdateSynopsis(c.TitlePath, synopsis, modified); err != nil {
		return err
	} else {
		c.Synopsis = synopsis
		c.Modified = modified
	}
	return nil
}

func (c *Chapter) SetContent(content string) error {
	modified := time.Now()
	if err := c.Printer.UpdateContent(c.TitlePath, content, modified); err != nil {
		return err
	} else {
		c.Content = content
		c.Modified = modified
	}
	return nil
}

func (c *Chapter) Delete() error {
	if err := c.Printer.Delete(c.TitlePath); err != nil {
		log.Println(err)
		return err
	}
	return nil
}

func (c *Chapter) Publish(publish bool) error {
	if err := c.Printer.Publish(c.TitlePath, publish); err != nil {
		log.Println(err)
		return err
	}
	c.IsPublished = publish
	return nil
}

func (c *Chapter) SetImg(img Img) error {
	if err := c.Printer.WriteImg(c.TitlePath, img); err != nil {
		log.Println(err)
		return err
	}
	c.Img = img
	return nil
}

func (c *Chapter) SetImgs(imgs []Img) error {
	if err := c.Printer.WriteImgs(c.TitlePath, imgs); err != nil {
		log.Println(err)
		return err
	}
	c.Imgs = imgs
	return nil
}
