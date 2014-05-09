package mongo

import (
	"log"
	"time"

	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"

	"code.minty.io/config"
	articles "code.minty.io/wombat-articles"
	"code.minty.io/wombat/backends"
)

type Backend struct {
	database, collection string
	session              *mgo.Session
	NewArticle           ArticleFn
	NewArticles          ArticlesFn
	SetPrinter           PrinterFn
	SetPrinters          PrintersFn
}

type QueryFunc func(c *mgo.Collection)
type ArticleFn func() interface{}
type ArticlesFn func(limit int) interface{}
type PrinterFn func(o interface{}, p articles.Printer)
type PrintersFn func(o interface{}, p articles.Printer)

func init() {
	// Mongo URL
	url, ok := config.GroupString("db", "mongoURL")
	if !ok {
		log.Fatal("wombat:apps:article: MongoURL missing from configuration")
	}

	// Database name
	db, ok := config.GroupString("db", "mongoDB")
	if !ok {
		db = "main"
	}

	col, ok := config.GroupString("db", "mongoCol")
	if !ok {
		col = "articles"
	}

	// Mongo error
	b, err := New(url, db, col)
	if err != nil {
		log.Fatal("Failed to create backend: %v", err)
	}

	// Register
	backends.Register("wombat:apps:article-reader", b)
	backends.Register("wombat:apps:article-printer", b)
}

func newArticle() interface{} {
	return new(articles.Article)
}

func newArticles(limit int) interface{} {
	s := make([]articles.Article, 0, limit)
	return &s
}

func setPrinter(o interface{}, p articles.Printer) {
	if a, o := o.(*articles.Article); o {
		a.Printer = p
	}
}
func setPrinters(o interface{}, p articles.Printer) {
	if s, ok := o.(*[]articles.Article); ok {
		for _, a := range *s {
			a.Printer = p
		}
	}
}

func New(url, database, collection string) (Backend, error) {
	var b Backend
	session, err := mgo.Dial(url)
	if err != nil {
		return b, err
	}

	session.SetMode(mgo.Monotonic, true)
	b = Backend{database, collection, session, newArticle, newArticles, setPrinter, setPrinters}
	return b, nil
}

func (b Backend) Col() (*mgo.Session, *mgo.Collection) {
	s := b.session.New()
	//return s, s.DB(db).C(COL_NAME)
	//return s, s.DB(db).C(b.collection)
	return s, s.DB(b.database).C(b.collection)
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
	b.SetPrinter(c, b)
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
		Sort("-created").
		Skip(page * limit).
		Limit(limit).
		All(c); err != nil {
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query article list", err)
	}
	b.SetPrinter(c, b)

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
