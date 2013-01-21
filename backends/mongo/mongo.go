package mongo

import (
	"errors"
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
	db = "main"
)

type Backend struct {
	session *mgo.Session
}

type queryFunc func(c *mgo.Collection)

func init() {
	if url, ok := config.GroupString("db", "mongoURL"); !ok {
		log.Fatal("apps-chapters mongo: MongoURL missing from configuration")
	} else if session, err := mgo.Dial(url); err != nil {
		log.Fatal("Failed to retrieve Mongo session: ", err)
	} else {
		// set monotonic mode
		session.SetMode(mgo.Monotonic, true)
		// register backend
		b := Backend{session}
		backends.Register("mongo:apps:chapter-reader", b)
		backends.Register("mongo:apps:chapter-printer", b)
	}

	if d, ok := config.GroupString("db", "mongoDB"); ok {
		db = d
	}
}

func (b Backend) col() (*mgo.Session, *mgo.Collection) {
	s := b.session.New()
	return s, s.DB(db).C(COL_NAME)
}
func (b Backend) query(fn queryFunc) {
	s, c := b.col()
	defer s.Close()
	fn(c)
}

// Reader
func (b Backend) ByTitlePath(titlePath string) (interface{}, error) {
	s, col := b.col()
	defer s.Close()

	c := new(chapters.Chapter)
	if err := col.Find(bson.M{"titlePath": titlePath}).One(&c); err != nil {
		return c, backends.NewError(backends.StatusNotFound, "Chapter not found", err)
	}
	c.Printer = b
	return *c, nil
}
func (b Backend) Recent(limit, page int, includeUnpublished bool) (interface{}, error) {
	s, col := b.col()
	defer s.Close()

	c := make([]chapters.Chapter, 0, limit)
	var q bson.M = nil
	if !includeUnpublished {
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
		All(&c); err != nil {
		return c, backends.NewError(backends.StatusDatastoreError, "Failed to query chapter list", err)
	}
	for i := range c {
		c[i].Printer = b
	}

	return c, nil
}

// Printer
func (b Backend) Print(chapter interface{}) error {
	c, ok := chapter.(*chapters.Chapter)
	if !ok {
		return errors.New("Doh!")
	}
	s, col := b.col()
	defer s.Close()

	if err := col.Insert(c); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to create chapter", err)
	}

	return nil
}
func (b Backend) UpdateContent(titlePath, content string, modified time.Time) error {
	s, col := b.col()
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
	s, col := b.col()
	defer s.Close()

	// update the chapter's content
	selector := bson.M{"titlePath": titlePath}
	if err := col.Remove(selector); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to remove chapter", err)
	}
	return nil
}
func (b Backend) Publish(titlePath string, publish bool) error {
	session, col := b.col()
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
	/*i, ok := img.(chapters.Img)
	if !ok {
		return errors.New("Doh!")
	}*/

	session, col := b.col()
	defer session.Close()

	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"img": img}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update image/thumb", err)
	}
	return nil
}
func (b Backend) WriteImgs(titlePath string, imgs interface{}) error {
	/*i, ok := imgs.([]chapters.Img)
	if !ok {
		return errors.New("Doh!")
	} else {
		log.Println(i)
	}*/
	session, col := b.col()
	defer session.Close()

	selector := bson.M{"titlePath": titlePath}
	change := bson.M{"$set": bson.M{"imgs": imgs}} //&a.Imgs}}
	if err := col.Update(selector, change); err != nil {
		return backends.NewError(backends.StatusDatastoreError, "Failed to update images", err)
	}
	return nil
}
