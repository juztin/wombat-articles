package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"bitbucket.org/juztin/config"
	"bitbucket.org/juztin/dingo/request"
	"bitbucket.org/juztin/dingo/views"
	"bitbucket.org/juztin/imagery"
	"bitbucket.org/juztin/imagery/web"
	"bitbucket.org/juztin/wombat"
	articles "bitbucket.org/juztin/wombat-articles"
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
	MediaURL   string
	BasePath   string
	ImagePath  string
	Templates  map[string]string
}

type ItoArticle func(o interface{}) *articles.Article

func New() Handler {
	h := new(Handler)
	h.articles = articles.New()
	h.ItoArticle = baseArticle
	h.MediaURL = config.Required.GroupString("articles", "mediaURL")
	h.BasePath = config.Required.GroupString("articles", "basePath")
	h.ImagePath = config.Required.GroupString("articles", "imagePath")
	h.Templates = map[string]string{
		"list":   "/articles/articles.html",
		"view":   "/articles/article.html",
		"create": "/articles/create.html",
		"edit":   "/articles/edit.html",
	}
	return *h
}

/*----------------------------------Helpers-----------------------------------*/
// Security
func requireAdmin(fn wombat.Handler) wombat.Handler {
	return func(ctx wombat.Context) {
		if !ctx.User.IsAdmin() {
			ctx.HttpError(http.StatusUnauthorized)
			return
		}

		fn(ctx)
	}
}

func requireTitleAdmin(fn func(wombat.Context, string)) interface{} {
	return func(ctx wombat.Context, titlePath string) {
		if !ctx.User.IsAdmin() {
			ctx.HttpError(http.StatusUnauthorized)
			return
		}

		fn(ctx, titlePath)
	}
}

// ----------

func getErrorStr(r *http.Request, msg string) string {
	if request.IsApplicationJson(r) {
		msg = fmt.Sprintf(`{"error":"%s"}`, msg)
	}
	return msg
}

func getError(r *http.Request, err error) string {
	return getErrorStr(r, err.Error())
}

func IsImageRequest(ctx wombat.Context) bool {
	c := ctx.Header.Get("Content-Type")
	i := imgTypes.Search(c)
	return i < imgTypes.Len() && imgTypes[i] == c
}

func JsonMessage(ctx wombat.Context) (msg JSONMessage, err error) {
	var data []byte

	defer ctx.Body.Close()
	data, err = ioutil.ReadAll(ctx.Body)
	if err == nil {
		err = json.Unmarshal(data, &msg)
	}

	return
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

func JsonHandler(ctx wombat.Context, a *articles.Article, imagePath string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	// Get the JSONMessage from the request
	msg, err := JsonMessage(ctx)
	if err != nil {
		ctx.HttpError(http.StatusBadRequest, getError(ctx.Request, err))
	}

	// Perform the given action
	switch msg.Action {
	default:
		// Invalid/missing action
		ctx.HttpError(http.StatusBadRequest, getErrorStr(ctx.Request, "Invalid action"))
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
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
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
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
		return
	}

	oldThumb := filepath.Join(path, a.TitlePath, a.Img.Src)
	s := img.Bounds().Size()

	// Update the article's thumbnail
	if err = a.SetImg(articles.Img{filename, filename, s.X, s.Y}); err != nil {
		log.Println("Failed to persit new thumbnail: ", filename, " for article: ", a.TitlePath)
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
	} else {
		os.Remove(oldThumb)
		j := fmt.Sprintf(`{"w":%d,"h":%d}`, filename, s.X, s.Y)
		ctx.Response.Write([]byte(j))
	}
}
func ImageHandler(ctx wombat.Context, a *articles.Article, path, filename string) {
	ctx.Response.Header().Set("Content-Type", "application/json")

	// Save the image
	img, err := web.SaveImage(ctx.Request, ctx.Response, path, filename)
	if err != nil {
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
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
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
	} else {
		j := fmt.Sprintf(`{"w":%d,"h":%d}`, filename, s.X, s.Y)
		ctx.Response.Write([]byte(j))
	}
}

func baseArticle(o interface{}) (a *articles.Article) {
	if article, ok := o.(*articles.Article); ok {
		a = article
	}
	return
}

/*-----------------------------------Handler-----------------------------------*/
func (h Handler) AddRoutes(s wombat.Server) {
	// routes
	s.ReRouter(fmt.Sprintf("^%s/$", h.BasePath)).
		Get(h.GetArticles).
		Post(requireAdmin(h.PostArticles))

	s.RRouter(fmt.Sprintf("^%s/(\\d{4}/\\d{2}/\\d{2}/[a-zA-Z0-9-]+/)$", h.BasePath)).
		Get(h.GetArticle).
		//Post(requireTitleAdmin(h.PostArticle)).
		Put(requireTitleAdmin(h.PutArticle)).
		Delete(requireTitleAdmin(h.DeleteArticle))
}

func (h Handler) Article(titlePath string, unPublished bool) (a interface{}, ok bool) {
	if o, err := h.articles.ByTitlePath(titlePath, unPublished); err != nil {
		// Maybe we don't want to log this, just in case someone decides to be a jerk
		log.Println("couldn't get article: ", titlePath, " : ", err)
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

/*---------------Articles---------------*/
func (h Handler) GetArticles(ctx wombat.Context) {
	// TODO implement JSON handler
	var tmpl string
	var o interface{}

	switch view := ctx.FormValue("view"); {
	default:
		tmpl = "list"
		o, _ = h.articles.Recent(10, 0, ctx.User.IsAdmin())
	case view == "create" && ctx.User.IsAdmin():
		tmpl = "create"
	}

	data := h.Data(ctx, o, "")
	views.Execute(ctx.Context, h.Templates[tmpl], data)
}

func (h Handler) PostArticles(ctx wombat.Context) {
	if request.IsApplicationJson(ctx.Request) {
		ctx.Response.Header().Set("Content-Type", "application/json")
	}

	var err error
	title := ctx.FormValue("title")
	if title == "" {
		// Missing title
		ctx.HttpError(http.StatusBadRequest, getErrorStr(ctx.Request, "Missing title"))
	} else if title, err = CreateArticle(ctx, title); err != nil { // Create article
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
	}

	// When no error, and not a JSON request, issue redirect to new article
	if err != nil && !request.IsApplicationJson(ctx.Request) {
		ctx.Redirect(fmt.Sprintf("%s/%s", h.BasePath, title))
	}
}

/*---------------Article----------------*/
func (h Handler) GetArticle(ctx wombat.Context, titlePath string) {
	// TODO implement JSON handler
	isAdmin := ctx.User.IsAdmin()
	o, ok := h.Article(titlePath, isAdmin)
	if !ok {
		ctx.HttpError(http.StatusNotFound)
		return
	}

	tmpl := h.Templates["view"]
	if isAdmin && ctx.FormValue("view") == "edit" {
		tmpl = h.Templates["edit"]
	}

	data := h.Data(ctx, o, titlePath)
	views.Execute(ctx.Context, tmpl, data)
}

//func (h Handler) PostArticle(ctx wombat.Context, titlePath string) {}

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
		JsonHandler(ctx, a, h.ImagePath)
	} else if IsImageRequest(ctx) {
		// Get the name of the image, or random name if missing/empty
		filename := ctx.FormValue("name")
		if filename == "" {
			filename = web.RandName(5)
		}

		// Save the thumbnail, or image
		path := filepath.Join(h.ImagePath, a.TitlePath)
		if t := ctx.FormValue("type"); t == "thumb" {
			ThumbHandler(ctx, a, path, "thumb."+filename)
		} else {
			ImageHandler(ctx, a, path, filename)
		}

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
		ctx.HttpError(http.StatusInternalServerError, getError(ctx.Request, err))
	}
}
