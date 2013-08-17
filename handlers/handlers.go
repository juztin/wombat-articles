package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"bitbucket.org/juztin/config"
	"bitbucket.org/juztin/dingo/views"
	"bitbucket.org/juztin/wombat"
	"bitbucket.org/juztin/wombat/apps/articles"
	"bitbucket.org/juztin/wombat/template/data"
)

type postHandler func(ctx wombat.Context, action, titlePath string)

type articleData struct {
	data.Data
	Article         interface{}
	ArticleMediaURL string
}

type articlesData struct {
	data.Data
	Articles        interface{}
	ArticleMediaURL string
}

type articleFn func(article interface{}) *articles.Article
type dataFn func(ctx wombat.Context, article interface{}, titlePath string, isSingle bool) interface{}

var (
	ArticleFn   articleFn
	DataFn      dataFn
	PostHandler postHandler
	reader      articles.Articles
	imgRoot     string
	media       string
	articlePath string
	listView    string
	articleView string
	createView  string
	updateView  string
)

func Init(s wombat.Server, basePath, list, article, create, update string) {
	ArticleFn = coreArticle
	DataFn = coreData
	reader = articles.New()
	imgRoot, _ = config.GroupString("articles", "imgRoot")
	media, _ = config.GroupString("articles", "media")

	articlePath = basePath
	listView = list
	articleView = article
	createView = create
	updateView = update

	// routes
	s.ReRouter(fmt.Sprintf("^%s/$", articlePath)).
		Get(listArticles).
		Post(newArticle)

	s.RRouter(fmt.Sprintf("^%s/(\\d{4}/\\d{2}/\\d{2}/[a-zA-Z0-9-]+/)$", articlePath)).
		Get(GetArticle).
		Post(postArticle)
}

func coreArticle(o interface{}) (c *articles.Article) {
	if article, ok := o.(*articles.Article); ok {
		c = article
	}
	return
}

func coreData(ctx wombat.Context, article interface{}, titlePath string, isSingle bool) interface{} {
	if titlePath == "" {
		return &articlesData{data.New(ctx), article, media}
	}
	return &articleData{data.New(ctx), article, media + titlePath}
}

func Article(titlePath string, unPublished bool) (c interface{}, ok bool) {
	// Maybe we don't want to log this, just in case someone decides to be a jerk
	/*if o, err := reader.ByTitlePath(titlePath, unPublished); err != nil {
		log.Println("couldn't get article: ", titlePath, " : ", err)
	} else {
		c, ok = o, true
	}*/
	if o, err := reader.ByTitlePath(titlePath, unPublished); err == nil {
		c, ok = o, true
	}
	return
}

/* -------------------------------- Handlers -------------------------------- */
func listArticles(ctx wombat.Context) {
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action == "create" {
			d := DataFn(ctx, nil, "", false)
			views.Execute(ctx.Context, createView, d)
			return
		}
	}

	c, _ := reader.Recent(10, 0, ctx.User.IsAdmin())
	d := DataFn(ctx, c, "", false)
	views.Execute(ctx.Context, listView, d)
}

func newArticle(ctx wombat.Context) {
	if ctx.User.IsAdmin() {
		if t, ok := Create(ctx); ok {
			//ctx.Redirect(fmt.Sprintf("%s/%s?action=update", articlePath, t))
			ctx.Redirect(fmt.Sprintf("%s/%s", articlePath, t))
		}
	}
	d := DataFn(ctx, nil, "", false)
	views.Execute(ctx.Context, listView, d)
}

/*func renderArticle(ctx wombat.Context, c interface{}) {
	d := &articleData{data.New(ctx), c}
	views.Execute(ctx.Context, updateView, d)
}*/

func GetArticle(ctx wombat.Context, titlePath string) {
	isAdmin := ctx.User.IsAdmin()
	c, ok := Article(titlePath, isAdmin)
	if !ok {
		ctx.HttpError(404)
		return
	}

	v := articleView
	if isAdmin {
		if action := ctx.FormValue("action"); action == "update" {
			v = updateView
		}
	}

	d := DataFn(ctx, c, titlePath, true)
	views.Execute(ctx.Context, v, d)
}

func postArticle(ctx wombat.Context, titlePath string) {
	if ctx.User.IsAdmin() {
		if action := ctx.FormValue("action"); action != "" {
			switch action {
			default:
				//GetArticle(ctx, titlePath)
				if PostHandler != nil {
					PostHandler(ctx, action, titlePath)
				} else {
					GetArticle(ctx, titlePath)
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
			GetArticle(ctx, titlePath)
		}
	} else {
		GetArticle(ctx, titlePath)
	}
}

/* ------------------------------------  ------------------------------------ */

func updateSynopsisContent(ctx wombat.Context, titlePath, key string) {
	o, ok := Article(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ArticleFn(o)

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
		ctx.Response.Header().Set("Content-Type", "application/json")
		if j, err := json.Marshal(map[string]string{key: s}); err != nil {
			log.Println("Failed to marshal article's `", key, "` to JSON : ", err)
			ctx.HttpError(500)
			return
		} else {
			ctx.Response.Write(j)
		}
	} else {
		//renderArticle(ctx, *a)
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

	c := articles.NewArticle(title)
	if err := c.Print(); err != nil {
		fmt.Println("no article: ", err)
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
	//getArticle(ctx, titlePath)
}

func ImgHandler(ctx wombat.Context, titlePath string, isThumb bool) {
	// get the article
	o, ok := Article(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ArticleFn(o)

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
	var imgs []articles.Img
	exists := false
	s := i.Bounds().Size()
	if isThumb {
		// TODO -> maybe remove the image upon successful addition of the new one
		// remove current thumb
		imgPath := filepath.Join(imgRoot, titlePath)
		os.Remove(filepath.Join(imgPath, c.Img.Src))
	} else {
		l := len(c.Imgs)
		imgs = make([]articles.Img, l, l+1)
		copy(imgs, c.Imgs)
		for _, v := range imgs {
			if v.Src == n {
				v.W, v.H = s.X, s.Y
				exists = true
			}
		}
		if !exists {
			imgs = append(imgs, articles.Img{n, "", s.X, s.Y})
		}
	}

	// update article images
	if isThumb {
		err = c.SetImg(articles.Img{n, imgName, s.X, s.Y})
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
		ctx.Response.Header().Set("Content-Type", "application/json")
		k := "image"
		if isThumb {
			k = "thumb"
		}
		j := fmt.Sprintf(`{"%s":"%s","w":%d,"h":%d}`, k, n, s.X, s.Y)
		ctx.Response.Write([]byte(j))
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
	// get the article
	o, ok := Article(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ArticleFn(o)

	// get the image to be deleted
	src := ctx.FormValue("image")

	// update the articles images
	n := []articles.Img{}
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
		http.Redirect(ctx.Response, ctx.Request, ctx.Request.Referer(), 303)
	}
}

func SetActive(ctx wombat.Context, titlePath string) {
	// get the article
	o, ok := Article(titlePath, true)
	if !ok {
		ctx.HttpError(404)
		return
	}
	c := ArticleFn(o)

	isActive, _ := strconv.ParseBool(ctx.FormValue("active"))
	if err := c.Publish(isActive); err != nil {
		ctx.HttpError(500)
		return
	}

	if d := ctx.FormValue("d"); d != "json" {
		http.Redirect(ctx.Response, ctx.Request, ctx.Request.Referer(), 303)
	}
}
