package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"bitbucket.org/juztin/dingo/views"
	"bitbucket.org/juztin/wombat"
	"bitbucket.org/juztin/wombat/apps/chapters"
	"bitbucket.org/juztin/wombat/config"
	"bitbucket.org/juztin/wombat/template/data"
)

type chapterData struct {
	data.Data
	Chapter chapters.Chapter
}

type chaptersData struct {
	data.Data
	Chapters []chapters.Chapter
}

var (
	reader      chapters.Chapters
	imgRoot     string
	chapterPath string
	listView    string
	chapterView string
	createView  string
	updateView  string
)

func Init(s wombat.Server, basePath, list, chapter, create, update string) {
	reader = chapters.New()
	imgRoot, _ = config.GetString("ArticleImgRoot")

	chapterPath = basePath
	listView = list
	chapterView = chapter
	createView = create
	updateView = update

	// routes
	s.ReRouter(fmt.Sprintf("^%s/$", chapterPath)).
		Get(listChapters).
		Post(newChapter)

	s.RRouter(fmt.Sprintf("^%s(/\\d{4}/\\d{2}/\\d{2}/[a-zA-Z0-9-]+/)$", chapterPath)).
		Get(getChapter).
		Post(postChapter)
}

func chapter(titlePath string) (c chapters.Chapter, ok bool) {
	if o, err := reader.ByTitlePath(titlePath); err != nil {
		log.Println("couldn't get chapter: ", titlePath, " : ", err)
	} else {
		c, ok = o.(chapters.Chapter)
	}
	return
}

/* -------------------------------- Handlers -------------------------------- */
func listChapters(ctx wombat.Context) {
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action == "create" {
			views.Execute(ctx.Context, createView, data.New(ctx))
			return
		}
	}

	c, _ := reader.Recent(10, 0, ctx.User.IsAdmin())
	d := &chaptersData{data.New(ctx), c.([]chapters.Chapter)}
	views.Execute(ctx.Context, listView, d)
}

func newChapter(ctx wombat.Context) {
	if ctx.User.IsAdmin() {
		if t, ok := Create(ctx); ok {
			ctx.Redirect(chapterPath + t)
		}
	}
	views.Execute(ctx.Context, listView, data.New(ctx))
}

func renderChapter(ctx wombat.Context, c chapters.Chapter) {
	d := &chapterData{data.New(ctx), c}
	views.Execute(ctx.Context, updateView, d)
}

func getChapter(ctx wombat.Context, titlePath string) {
	c, ok := chapter(titlePath)
	if !ok {
		ctx.HttpError(404)
		return
	}

	v := chapterView
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action == "update" {
			v = updateView
		}
	} else if !c.IsPublished {
		ctx.HttpError(404)
		return
	}

	d := &chapterData{data.New(ctx), c}
	views.Execute(ctx.Context, v, d)
}

func postChapter(ctx wombat.Context, titlePath string) {
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action != "" {
			switch action {
			default:
				getChapter(ctx, titlePath)
			case "update":
				UpdateContent(ctx, titlePath)
			case "delete":
				Delete(ctx, titlePath)
			case "addImage":
				AddImage(ctx, titlePath)
			case "addThumb":
				AddThumb(ctx, titlePath)
			case "delImage":
				DelImage(ctx, titlePath)
			case "setActive":
				SetActive(ctx, titlePath)
			}
		} else {
			getChapter(ctx, titlePath)
		}
	} else {
		getChapter(ctx, titlePath)
	}
}

/* ------------------------------------  ------------------------------------ */

func Create(ctx wombat.Context) (string, bool) {
	title := ctx.FormValue("title")
	if title == "" {
		fmt.Println("no title")
		return title, false
	}

	c := chapters.NewChapter(title)
	if err := c.Print(); err != nil {
		fmt.Println("no chapter: ", err)
		return "", false
	}
	return c.TitlePath, true
}

//func UpdateContent(ctx wombat.Context, titlePath string, article *interface{}) error {
func UpdateContent(ctx wombat.Context, titlePath string) {
	c, ok := chapter(titlePath)
	if !ok {
		ctx.HttpError(404)
		return
	}

	// get the content
	content := ctx.FormValue("content")
	c.UpdateContent(content)

	// render either JSON|HTML
	if d := ctx.FormValue("d"); d == "json" {
		// return the new article's content (json)
		ctx.Writer.Header().Set("Content-Type", "application/json")
		if j, err := json.Marshal(map[string]string{"content": content}); err != nil {
			log.Println("Failed to marshal article's content to JSON : ", err)
			ctx.HttpError(500)
			return
		} else {
			ctx.Writer.Write(j)
		}
	} else {
		//renderChapter(ctx, *a)
	}
}

func Delete(ctx wombat.Context, titlePath string) {
	//getChapter(ctx, titlePath)
}

func ImgHandler(ctx wombat.Context, titlePath string, isThumb bool) {
	// get the chapter
	c, ok := chapter(titlePath)
	if !ok {
		ctx.HttpError(404)
		return
	}

	// create the image, from the POST
	imgName, f, err := formFileImage(ctx, titlePath)
	if err != nil {
		log.Println("Failed to create temporary image from form-file: ", titlePath, " : ", err)
		ctx.HttpError(500)
		return
	}

	// convert image to jpeg
	n, i, err := convertToJpg(imgName, f, isThumb)
	if err != nil {
		log.Println("Failed to convert image to jpeg for article: ", titlePath, " : ", err)
		ctx.HttpError(500)
		return
	}

	// create the image object
	var imgs []chapters.Img
	exists := false
	s := i.Bounds().Size()
	if isThumb {
		// TODO -> maybe remove the image upon successful addition of the new one
		// remove current thumb
		imgPath := filepath.Join(imgRoot, titlePath)
		os.Remove(filepath.Join(imgPath, c.Img.Src))
	} else {
		l := len(c.Imgs)
		imgs = make([]chapters.Img, l, l+1)
		copy(imgs, c.Imgs)
		for _, v := range imgs {
			if v.Src == n {
				v.W, v.H = s.X, s.Y
				exists = true
			}
		}
		if !exists {
			imgs = append(imgs, chapters.Img{n, "", s.X, s.Y})
		}
	}

	// update article images
	if isThumb {
		err = c.SetImg(chapters.Img{n, imgName, s.X, s.Y})
	} else {
		err = c.SetImgs(imgs)
	}

	if err != nil {
		log.Println("Failed to persit new image: ", imgName, " for article: ", titlePath)
		ctx.HttpError(500)
		return
	}

	// append the image to the article
	if !isThumb {
		c.Imgs = imgs
	}

	// return either a JSON/HTML response
	if d := ctx.FormValue("d"); d == "json" {
		ctx.Writer.Header().Set("Content-Type", "application/json")
		k := "image"
		if isThumb {
			k = "thumb"
		}
		j := fmt.Sprintf(`{"%s":"%s","w":%d,"h":%d}`, k, n, s.X, s.Y)
		ctx.Writer.Write([]byte(j))
	} else {
		//renderArticle(ctx, a)
	}
}

func AddThumb(ctx wombat.Context, titlePath string) {
	ImgHandler(ctx, titlePath, true)
}

func AddImage(ctx wombat.Context, titlePath string) {
	ImgHandler(ctx, titlePath, false)
}

func DelImage(ctx wombat.Context, titlePath string) {
	// get the chapter
	c, ok := chapter(titlePath)
	if !ok {
		ctx.HttpError(404)
		return
	}

	// get the image to be deleted
	src := ctx.FormValue("image")

	// update the articles images
	n := []chapters.Img{}
	for _, i := range c.Imgs {
		if i.Src != src {
			n = append(n, i)
		} else {
			imgPath := filepath.Join(imgRoot, titlePath)
			os.Remove(filepath.Join(imgPath, i.Src))
		}
		//p.Imgs = append(p.Imgs[:i], p.Imgs[i+1:]...)
	}

	// if a matching image was found, remove it
	if len(n) != len(c.Imgs) {
		if err := c.SetImgs(n); err != nil {
			log.Println("Failed to persit image deletion: ", src, " for article: ", titlePath)
			ctx.HttpError(500)
			return
		}
	}

	// redirect back to the update page, when a regular POST
	if d := ctx.FormValue("d"); d != "json" {
		http.Redirect(ctx.Writer, ctx.Request, ctx.Request.Referer(), 303)
	}
}

func SetActive(ctx wombat.Context, titlePath string) {
	// get the chapter
	c, ok := chapter(titlePath)
	if !ok {
		ctx.HttpError(404)
		return
	}

	isActive, _ := strconv.ParseBool(ctx.FormValue("active"))
	if err := c.Publish(isActive); err != nil {
		ctx.HttpError(500)
		return
	}

	if d := ctx.FormValue("d"); d != "json" {
		http.Redirect(ctx.Writer, ctx.Request, ctx.Request.Referer(), 303)
	}
}
