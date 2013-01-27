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

type postHandler func(ctx wombat.Context, action, titlePath string)

type chapterData struct {
	data.Data
	Chapter         interface{}
	ChapterMediaURL string
}

type chaptersData struct {
	data.Data
	Chapters        interface{}
	ChapterMediaURL string
}

type chapterFn func(chapter interface{}) chapters.Chapter
type dataFn func(ctx wombat.Context, chapter interface{}, titlePath string, isSingle bool) interface{}

var (
	ChapterFn   chapterFn
	DataFn      dataFn
	PostHandler postHandler
	reader      chapters.Chapters
	imgRoot     string
	media       string
	chapterPath string
	listView    string
	chapterView string
	createView  string
	updateView  string
)

func Init(s wombat.Server, basePath, list, chapter, create, update string) {
	ChapterFn = coreChapter
	DataFn = coreData
	reader = chapters.New()
	imgRoot, _ = config.GroupString("chapters", "imgRoot")
	media, _ = config.GroupString("chapters", "media")

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
		Get(GetChapter).
		Post(postChapter)
}

func coreChapter(o interface{}) (c chapters.Chapter) {
	if chapter, ok := o.(chapters.Chapter); ok {
		c = chapter
	}
	return
}

func coreData(ctx wombat.Context, chapter interface{}, titlePath string, isSingle bool) interface{} {
	if titlePath == "" {
		return &chaptersData{data.New(ctx), chapter, media}
	}
	return &chapterData{data.New(ctx), chapter, media + titlePath[1:]}
}

func Chapter(titlePath string, unPublished bool) (c interface{}, ok bool) {
	// Maybe we don't want to log this, just in case someone decides to be a jerk
	/*if o, err := reader.ByTitlePath(titlePath, unPublished); err != nil {
		log.Println("couldn't get chapter: ", titlePath, " : ", err)
	} else {
		c, ok = o, true
	}*/
	if o, err := reader.ByTitlePath(titlePath, unPublished); err == nil {
		c, ok = o, true
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
	d := DataFn(ctx, c, "", false)
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

/*func renderChapter(ctx wombat.Context, c interface{}) {
	d := &chapterData{data.New(ctx), c}
	views.Execute(ctx.Context, updateView, d)
}*/

func GetChapter(ctx wombat.Context, titlePath string) {
	isAdmin := ctx.User.IsAdmin()
	c, ok := Chapter(titlePath, isAdmin)
	if !ok {
		ctx.HttpError(404)
		return
	}

	v := chapterView
	if isAdmin {
		if action := ctx.FormValue("action"); action == "update" {
			v = updateView
		}
	}

	d := DataFn(ctx, c, titlePath, true)
	views.Execute(ctx.Context, v, d)
}

func postChapter(ctx wombat.Context, titlePath string) {
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action != "" {
			switch action {
			default:
				//GetChapter(ctx, titlePath)
				if PostHandler != nil {
					PostHandler(ctx, action, titlePath)
				} else {
					GetChapter(ctx, titlePath)
				}
			case "update":
				update(ctx, titlePath)
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
			GetChapter(ctx, titlePath)
		}
	} else {
		GetChapter(ctx, titlePath)
	}
}

/* ------------------------------------  ------------------------------------ */

func updateSynopsisContent(ctx wombat.Context, titlePath, key string) {
	o, ok := Chapter(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ChapterFn(o)

	// get the value
	s := ctx.FormValue(key)
	switch key {
	case "content":
		c.SetContent(s)
	case "synopsis":
		c.SetSynopsis(s)
	}

	// render either JSON|HTML
	if d := ctx.FormValue("d"); d == "json" {
		// return the new article's (json)
		ctx.Writer.Header().Set("Content-Type", "application/json")
		if j, err := json.Marshal(map[string]string{key: s}); err != nil {
			log.Println("Failed to marshal article's `", key, "` to JSON : ", err)
			ctx.HttpError(500)
			return
		} else {
			ctx.Writer.Write(j)
		}
	} else {
		//renderChapter(ctx, *a)
	}
}

func update(ctx wombat.Context, titlePath string) {
	if ctx.Form == nil {
		ctx.ParseMultipartForm(2 << 20)
	}

	if _, ok := ctx.Form["content"]; ok {
		UpdateContent(ctx, titlePath)
	} else if _, ok := ctx.Form["synopsis"]; ok {
		UpdateSynopsis(ctx, titlePath)
	}
}

/* ---------- */

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

func UpdateSynopsis(ctx wombat.Context, titlePath string) {
	updateSynopsisContent(ctx, titlePath, "synopsis")
}

func UpdateContent(ctx wombat.Context, titlePath string) {
	updateSynopsisContent(ctx, titlePath, "content")
}

func Delete(ctx wombat.Context, titlePath string) {
	//getChapter(ctx, titlePath)
}

func ImgHandler(ctx wombat.Context, titlePath string, isThumb bool) {
	// get the chapter
	o, ok := Chapter(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ChapterFn(o)

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
	o, ok := Chapter(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ChapterFn(o)

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
	o, ok := Chapter(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ChapterFn(o)

	isActive, _ := strconv.ParseBool(ctx.FormValue("active"))
	if err := c.Publish(isActive); err != nil {
		ctx.HttpError(500)
		return
	}

	if d := ctx.FormValue("d"); d != "json" {
		http.Redirect(ctx.Writer, ctx.Request, ctx.Request.Referer(), 303)
	}
}
