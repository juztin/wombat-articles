package mongo

import (
	"log"
	"time"

	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"

	"bitbucket.org/juztin/wombat/apps/chapters"
	"bitbucket.org/juztin/wombat/backends"
	"bitbucket.org/juztin/wombat/config"
)

const COL_NAME = "chapters"

var (
	db      = "main"
	backend Backend
)

type Backend struct {
	session     *mgo.Session
	NewChapter  ChapterFn
	NewChapters ChaptersFn
	SetPrinter  PrinterFn
	SetPrinters PrintersFn
}

type QueryFunc func(c *mgo.Collection)
type ChapterFn func() interface{}
type ChaptersFn func(limit int) interface{}
type PrinterFn func(o interface{})
type PrintersFn func(o interface{})

func init() {
	if url, ok := config.GroupString("db", "mongoURL"); !ok {
		log.Fatal("apps-chapters mongo: MongoURL missing from configuration")
	} else if session, err := mgo.Dial(url); err != nil {
		log.Fatal("Failed to retrieve Mongo session: ", err)
	} else {
		// set monotonic mode
		session.SetMode(mgo.Monotonic, true)
		// register backend
		backend = Backend{session, newChapter, newChapters, setPrinter, setPrinters}
		backends.Register("mongo:apps:chapter-reader", backend)
		backends.Register("mongo:apps:chapter-printer", backend)
	}

	if d, ok := config.GroupString("db", "mongoDB"); ok {
		db = d
	}
}

func newChapter() interface{} {
	return new(chapters.Chapter)
}

func newChapters(limit int) interface{} {
	s := make([]chapters.Chapter, 0, limit)
	return &s
}

func setPrinter(o interface{}) {
	if c, o := o.(*chapters.Chapter); o {
		c.Printer = backend
	}
}
func setPrinters(o interface{}) {
	if s, ok := o.(*[]chapters.Chapter); ok {
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

	c := b.NewChapter()
	query := bson.M{"titlePath": titlePath}
	if !unPublished {
		query["isPublished"] = true
	}
	if err := col.Find(query).One(c); err != nil {
		return c, backends.NewError(backends.StatusNotFound, "Chapter not found", err)
	}
	b.SetPrinter(c)
	return c, nil
}
func (b Backend) Recent(limit, page int, unPublished bool) (interface{}, error) {
	s, col := b.Col()
	defer s.Close()

	c := b.NewChapters(limit)
	var q bson.M = nil
	if !unPublished {
		q = bson.M{"isPublished": true}
	}

	/*iter := col.Find(q).
		Sort("created").
		Skip(page * limit).
		Iter()

	i := new(chapters.Chapter)
	for iter.Next(&i) {
		i.Printer = b
		c = append(c, *i)
		i = new(chapters.Chapter)
	}
	if iter.Err() != nil {
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query chapter list", iter.Err())
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
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query chapter list", err)
	}
	b.SetPrinter(c)

	return c, nil
}

// Printer
func (b Backend) Print(chapter interface{}) error {
	s, col := b.Col()
	defer s.Close()

	if err := col.Insert(chapter); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to create chapter", err)
	}

	return nil
}
func (b Backend) UpdateSynopsis(titlePath, synopsis string, modified time.Time) error {
	s, col := b.Col()
	defer s.Close()

	// update the chapter's content
	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"synopsis": &synopsis, "modified": modified}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update chapter's synopsis", err)
	}
	return nil
}
func (b Backend) UpdateContent(titlePath, content string, modified time.Time) error {
	s, col := b.Col()
	defer s.Close()

	// update the chapter's content
	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"content": &content, "modified": modified}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update chapter's content", err)
	}
	return nil
}
func (b Backend) Delete(titlePath string) error {
	s, col := b.Col()
	defer s.Close()

	// update the chapter's content
	selector := bson.M{"titlePath": titlePath}
	if err := col.Remove(selector); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to remove chapter", err)
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
