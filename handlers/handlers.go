// Copyright 2013 Justin Wilson. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"bitbucket.org/juztin/config"
	"bitbucket.org/juztin/dingo/request"
	"bitbucket.org/juztin/dingo/views"
	"bitbucket.org/juztin/imagery"
	"bitbucket.org/juztin/imagery/web"
	"bitbucket.org/juztin/wombat"
	articles "bitbucket.org/juztin/wombat-articles"
	"bitbucket.org/juztin/wombat/backends"
	"bitbucket.org/juztin/wombat/template/data"
)

var imgTypes = sort.StringSlice{
	"image/gif",
	"image/jpeg",
	"image/png",
}

type ArticleData struct {
	data.Data
	Article         interface{}
	ArticleMediaURL string
}

type ArticlesData struct {
	data.Data
	Articles        interface{}
	ArticleMediaURL string
}

type JSONMessage struct {
	Action string `json:"action"`
	Data   string `json:"data"`
}

type Handler struct {
	articles   articles.Articles
	ItoArticle ItoArticle
	PageCount  int
	MediaURL   string
	BasePath   string
	ImagePath  string
	Templates  map[string]string
}

type Router interface {
	GetArticles(ctx wombat.Context)
	PostArticles(ctx wombat.Context)
	GetArticle(ctx wombat.Context, titlePath string)
	PutArticle(ctx wombat.Context, titlePath string)
	DeleteArticle(ctx wombat.Context, titlePath string)
}

type ItoArticle func(o interface{}) *articles.Article

func New() Handler {
	h := new(Handler)
	h.articles = articles.New()
	h.ItoArticle = baseArticle

	// URLs, Paths
	h.MediaURL = config.Required.GroupString("articles", "mediaURL")
	h.BasePath = config.Required.GroupString("articles", "basePath")
	h.ImagePath = config.Required.GroupString("articles", "imagePath")

	// Templates
	h.Templates = map[string]string{
		"list":   "/articles/articles.html",
		"view":   "/articles/article.html",
		"create": "/articles/create.html",
		"edit":   "/articles/edit.html",
	}

	// Page count
	h.PageCount = 30
	if c, ok := config.GroupInt("articles", "pageCount"); ok {
		h.PageCount = c
	}

	return *h
}

/*----------------------------------Helpers-----------------------------------*/
func baseArticle(o interface{}) (a *articles.Article) {
	if article, ok := o.(*articles.Article); ok {
		a = article
	}
	return
}

func RequireAdmin(fn wombat.Handler) wombat.Handler {
	return func(ctx wombat.Context) {
		if !ctx.User.IsAdmin() {
			if request.IsApplicationJson(ctx.Request) {
				ctx.Response.Header().Set("Content-Type", "application/json")
			}
			ctx.HttpError(http.StatusUnauthorized)
			return
		}

		fn(ctx)
	}
}

func RequireTitleAdmin(fn func(wombat.Context, string)) interface{} {
	return func(ctx wombat.Context, titlePath string) {
		if !ctx.User.IsAdmin() {
			if request.IsApplicationJson(ctx.Request) {
				ctx.Response.Header().Set("Content-Type", "application/json")
			}
			ctx.HttpError(http.StatusUnauthorized)
			return
		}

		fn(ctx, titlePath)
	}
}

func AddRoutes(s wombat.Server, r Router, basePath string) {
	// routes
	s.ReRouter(fmt.Sprintf("^%s/$", basePath)).
		Get(r.GetArticles).
		Post(RequireAdmin(r.PostArticles))

	s.RRouter(fmt.Sprintf("^%s/(\\d{4}/\\d{2}/\\d{2}/[a-zA-Z0-9-]+/)$", basePath)).
		Get(r.GetArticle).
		//Post(r.RequireTitleAdmin(h.PostArticle)).
		Put(RequireTitleAdmin(r.PutArticle)).
		Delete(RequireTitleAdmin(r.DeleteArticle))
}

func GetErrorStr(r *http.Request, msg string) string {
	if request.IsApplicationJson(r) {
		msg = fmt.Sprintf(`{"error":"%s"}`, msg)
	}
	return msg
}

func GetError(r *http.Request, err error) string {
	return GetErrorStr(r, err.Error())
}

func IsImageRequest(ctx wombat.Context) bool {
	c := ctx.Header.Get("Content-Type")
	i := imgTypes.Search(c)
	return i < imgTypes.Len() && imgTypes[i] == c
}

func CreateArticle(ctx wombat.Context, title string) (string, error) {
	a := articles.NewArticle(title)
	if err := a.Print(); err != nil {
		return "", err
	}

	return a.TitlePath, nil
}

func RemoveImage(a *articles.Article, src, imagePath string) (err error) {
	// New slice, minus the `src` image
	imgs := make([]articles.Img, 0, len(a.Imgs))
	//imgs := []articles.Img{}
	found := false
	for _, i := range a.Imgs {
		if i.Src == src {
			found = true
		} else {
			imgs = append(imgs, i)
		}
	}

	// If the `src` image was found, remove it from the Article
	if found {
		// Remove the `src` image from the filesystem
		if err = a.SetImgs(imgs); err == nil {
			p := filepath.Join(imagePath, a.TitlePath)
			os.Remove(filepath.Join(p, src))
		}
	}
	return
}

/*----------------------------------Handlers----------------------------------*/

func JSONHandler(ctx wombat.Context, a *articles.Article, imagePath string, data []byte) {
	// Get the JSONMessage from the request
	var msg JSONMessage
	err := json.Unmarshal(data, &msg)
	if err != nil {
		ctx.HttpError(http.StatusBadRequest, GetError(ctx.Request, err))
		return
	}

	// Perform the given action
	switch msg.Action {
	default:
		// Invalid/missing action
		err = errors.New("Invalid Action")
	case "setSynopsis":
		err = a.SetSynopsis(msg.Data)
	case "setContent":
		err = a.SetContent(msg.Data)
	case "setActive":
		// Toggle
		err = a.Publish(!a.IsPublished)
	case "deleteImage":
		err = RemoveImage(a, msg.Data, imagePath)
	}

	// Report if the action resulted in an error
	if err != nil {
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
	}
}

func ThumbHandler(ctx wombat.Context, a *articles.Article, path, filename string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	// Save & resize the image to a thumbnail
	img, err := web.SaveImage(ctx.Request, ctx.Response, path, filename)
	if err == nil {
		// Resize the image and save it
		if img, err = imagery.ResizeWidth(img, 200); err == nil {
			err = imagery.WriteTo(web.ImageType(ctx.Request), img, path, filename)
		}
	}
	if err != nil {
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
		return
	}

	oldThumb := filepath.Join(path, a.TitlePath, a.Img.Src)
	s := img.Bounds().Size()

	// Update the article's thumbnail
	if err = a.SetImg(articles.Img{filename, filename, s.X, s.Y}); err != nil {
		log.Println("Failed to persit new thumbnail: ", filename, " for article: ", a.TitlePath)
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
	} else {
		os.Remove(oldThumb)
		j := fmt.Sprintf(`{"thumb":"%s", "w":%d,"h":%d}`, filename, s.X, s.Y)
		ctx.Response.Write([]byte(j))
	}
}
func ImageHandler(ctx wombat.Context, a *articles.Article, path, filename string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	// Save the image
	img, err := web.SaveImage(ctx.Request, ctx.Response, path, filename)
	if err != nil {
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
		return
	}

	s := img.Bounds().Size()
	exists := false
	// Add or Update the image
	for _, v := range a.Imgs {
		if v.Src == filename {
			v.W, v.H = s.X, s.Y
			exists = true
		}
	}
	if !exists {
		a.Imgs = append(a.Imgs, articles.Img{filename, "", s.X, s.Y})
	}

	// Update the article's images
	if err = a.SetImgs(a.Imgs); err != nil {
		log.Println("Failed to persit new image: ", filename, " for article: ", a.TitlePath)
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
	} else {
		j := fmt.Sprintf(`{"image":"%s", "w":%d,"h":%d}`, filename, s.X, s.Y)
		ctx.Response.Write([]byte(j))
	}
}

func ImagesHandler(ctx wombat.Context, a *articles.Article, imagePath string) {
	// Get the name of the image, or random name if missing/empty
	filename := ctx.FormValue("name")
	if filename == "" {
		filename = web.RandName(5)
	}

	// Save the thumbnail, or image
	path := filepath.Join(imagePath, a.TitlePath)
	if t := ctx.FormValue("type"); t == "thumb" {
		ThumbHandler(ctx, a, path, "thumb."+filename)
	} else {
		ImageHandler(ctx, a, path, filename)
	}
}

func articleResponse(ctx wombat.Context, h *Handler, o interface{}, tmpl, title string) {
	if request.IsApplicationJson(ctx.Request) {
		// JSON
		ctx.Response.Header().Set("Content-Type", "application/json")
		jd, _ := json.Marshal(o)
		ctx.Response.Write(jd)
	} else {
		// HTTP
		data := h.Data(ctx, o, title)
		views.Execute(ctx.Context, h.Templates[tmpl], data)
	}
}

/*------------------------Handler-------------------------*/

func (h Handler) Article(titlePath string, unPublished bool) (a interface{}, ok bool) {
	o, err := h.articles.ByTitlePath(titlePath, unPublished)
	if err != nil {
		if err, ok := err.(backends.Error); ok && err.Status() != backends.StatusNotFound {
			log.Println(err)
		}
	} else {
		a, ok = o, true
	}

	return
}

func (h Handler) Data(ctx wombat.Context, article interface{}, titlePath string) interface{} {
	if titlePath == "" {
		return &ArticlesData{data.New(ctx), article, h.MediaURL}
	}
	return &ArticleData{data.New(ctx), article, h.MediaURL + titlePath}
}

/*----------Articles----------*/
func (h Handler) GetArticles(ctx wombat.Context) {
	var tmpl string
	var o interface{}

	switch view := ctx.FormValue("view"); {
	default:
		tmpl = "list"
		page, err := strconv.Atoi(ctx.FormValue("page"))
		if err != nil {
			page = 0
		}
		o, _ = h.articles.Recent(h.PageCount, page, ctx.User.IsAdmin())
	case view == "create" && ctx.User.IsAdmin():
		tmpl = "create"
	}

	// Handle HTTP/JSON response
	articleResponse(ctx, &h, o, tmpl, "")
}

func (h Handler) PostArticles(ctx wombat.Context) {
	title := ctx.FormValue("title")
	if request.IsApplicationJson(ctx.Request) {
		ctx.Response.Header().Set("Content-Type", "application/json")
		defer ctx.Body.Close()
		if b, err := ioutil.ReadAll(ctx.Body); err != nil {
			m := make(map[string]interface{})
			json.Unmarshal(b, &m)
			title, _ = m["title"].(string)
		}
	}

	if title == "" {
		// Missing title
		ctx.HttpError(http.StatusBadRequest, GetErrorStr(ctx.Request, "Missing title"))
		return
	}

	t, err := CreateArticle(ctx, title)
	if err != nil {
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
		return
	}

	// When not a JSON request issue redirect to new article
	if !request.IsApplicationJson(ctx.Request) {
		ctx.Redirect(fmt.Sprintf("%s/%s?view=edit", h.BasePath, t))
	}
}

/*----------Article-----------*/
func (h Handler) GetArticle(ctx wombat.Context, titlePath string) {
	isAdmin := ctx.User.IsAdmin()
	o, ok := h.Article(titlePath, isAdmin)
	if !ok {
		ctx.HttpError(http.StatusNotFound)
		return
	}

	tmpl := "view"
	if isAdmin && ctx.FormValue("view") == "edit" {
		tmpl = "edit"
	}

	// Handle HTTP/JSON response
	articleResponse(ctx, &h, o, tmpl, titlePath)
}

func (h Handler) PutArticle(ctx wombat.Context, titlePath string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	o, ok := h.Article(titlePath, true)
	if !ok {
		ctx.HttpError(http.StatusNotFound)
		return
	}
	a := h.ItoArticle(o)

	// JSON message
	if request.IsApplicationJson(ctx.Request) {
		// Get the bytes for JSON processing
		defer ctx.Body.Close()
		if data, err := ioutil.ReadAll(ctx.Body); err != nil {
			ctx.HttpError(http.StatusBadRequest, GetError(ctx.Request, err))
		} else {
			JSONHandler(ctx, a, h.ImagePath, data)
		}
	} else if IsImageRequest(ctx) {
		ImagesHandler(ctx, a, h.ImagePath)
	} else {
		// Nothing could be done for the given request
		ctx.HttpError(http.StatusBadRequest)
	}
}

func (h Handler) DeleteArticle(ctx wombat.Context, titlePath string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	o, ok := h.Article(titlePath, true)
	if !ok {
		ctx.HttpError(http.StatusNotFound)
		return
	}

	a := h.ItoArticle(o)
	if err := a.Delete(); err != nil {
		ctx.HttpError(http.StatusInternalServerError, GetError(ctx.Request, err))
	}
}
