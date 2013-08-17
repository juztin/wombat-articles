package mongo

import (
	"log"
	"time"

	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"

	"bitbucket.org/juztin/config"
	"bitbucket.org/juztin/wombat/apps/articles"
	"bitbucket.org/juztin/wombat/backends"
)

const COL_NAME = "articles"

var (
	db      = "main"
	backend Backend
)

type Backend struct {
	session     *mgo.Session
	NewArticle  ArticleFn
	NewArticles ArticlesFn
	SetPrinter  PrinterFn
	SetPrinters PrintersFn
}

type QueryFunc func(c *mgo.Collection)
type ArticleFn func() interface{}
type ArticlesFn func(limit int) interface{}
type PrinterFn func(o interface{})
type PrintersFn func(o interface{})

func init() {
	if url, ok := config.GroupString("db", "mongoURL"); !ok {
		log.Fatal("apps-articles mongo: MongoURL missing from configuration")
	} else if session, err := mgo.Dial(url); err != nil {
		log.Fatal("Failed to retrieve Mongo session: ", err)
	} else {
		// set monotonic mode
		session.SetMode(mgo.Monotonic, true)
		// register backend
		backend = Backend{session, newArticle, newArticles, setPrinter, setPrinters}
		backends.Register("mongo:apps:article-reader", backend)
		backends.Register("mongo:apps:article-printer", backend)
	}

	if d, ok := config.GroupString("db", "mongoDB"); ok {
		db = d
	}
}

func newArticle() interface{} {
	return new(articles.Article)
}

func newArticles(limit int) interface{} {
	s := make([]articles.Article, 0, limit)
	return &s
}

func setPrinter(o interface{}) {
	if c, o := o.(*articles.Article); o {
		c.Printer = backend
	}
}
func setPrinters(o interface{}) {
	if s, ok := o.(*[]articles.Article); ok {
		for _, c := range *s {
			c.Printer = backend
		}
	}
}

func (b Backend) Col() (*mgo.Session, *mgo.Collection) {
	s := b.session.New()
	return s, s.DB(db).C(COL_NAME)
}
func (b Backend) Query(fn QueryFunc) {
	s, c := b.Col()
	defer s.Close()
	fn(c)
}

// Reader
func (b Backend) ByTitlePath(titlePath string, unPublished bool) (interface{}, error) {
	s, col := b.Col()
	defer s.Close()

	c := b.NewArticle()
	query := bson.M{"titlePath": titlePath}
	if !unPublished {
		query["isPublished"] = true
	}
	if err := col.Find(query).One(c); err != nil {
		return c, backends.NewError(backends.StatusNotFound, "Article not found", err)
	}
	b.SetPrinter(c)
	return c, nil
}
func (b Backend) Recent(limit, page int, unPublished bool) (interface{}, error) {
	s, col := b.Col()
	defer s.Close()

	c := b.NewArticles(limit)
	var q bson.M = nil
	if !unPublished {
		q = bson.M{"isPublished": true}
	}

	/*iter := col.Find(q).
		Sort("created").
		Skip(page * limit).
		Iter()

	i := new(articles.Article)
	for iter.Next(&i) {
		i.Printer = b
		c = append(c, *i)
		i = new(articles.Article)
	}
	if iter.Err() != nil {
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query article list", iter.Err())
	}*/

	/* Using the below, instead of the above because:
	 *  according to `ab -c 35 -rn 1000 http://127.0.0.1:9991/articles/`
	 *  the the below is about 450 req/s faster
	 */
	if err := col.Find(q).
		Sort("created").
		Skip(page * limit).
		Limit(limit).
		All(c); err != nil {
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query article list", err)
	}
	b.SetPrinter(c)

	return c, nil
}

// Printer
func (b Backend) Print(article interface{}) error {
	s, col := b.Col()
	defer s.Close()

	if err := col.Insert(article); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to create article", err)
	}

	return nil
}
func (b Backend) UpdateSynopsis(titlePath, synopsis string, modified time.Time) error {
	s, col := b.Col()
	defer s.Close()

	// update the article's content
	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"synopsis": &synopsis, "modified": modified}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update article's synopsis", err)
	}
	return nil
}
func (b Backend) UpdateContent(titlePath, content string, modified time.Time) error {
	s, col := b.Col()
	defer s.Close()

	// update the article's content
	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"content": &content, "modified": modified}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update article's content", err)
	}
	return nil
}
func (b Backend) Delete(titlePath string) error {
	s, col := b.Col()
	defer s.Close()

	// update the article's content
	selector := bson.M{"titlePath": titlePath}
	if err := col.Remove(selector); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to remove article", err)
	}
	return nil
}
func (b Backend) Publish(titlePath string, publish bool) error {
	session, col := b.Col()
	defer session.Close()

	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"isPublished": publish}}
	if err := col.Update(selector, change); err != nil {
		log.Println(err)
		return backends.NewError(backends.StatusDatastoreError, "Failed to update published status", err)
	}
	return nil
}
func (b Backend) WriteImg(titlePath string, img interface{}) error {
	session, col := b.Col()
	defer session.Close()

	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"img": img}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update image/thumb", err)
	}
	return nil
}
func (b Backend) WriteImgs(titlePath string, imgs interface{}) error {
	session, col := b.Col()
	defer session.Close()

	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"imgs": imgs}} //&a.Imgs}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update images", err)
	}
	return nil
}
