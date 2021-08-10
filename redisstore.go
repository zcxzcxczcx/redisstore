package redisstore

import (
	"bytes"
	"encoding/base32"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	ginsessions "github.com/gin-gonic/contrib/sessions"
	"github.com/go-redis/redis"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// SessionSerializer provides an interface hook for alternative serializers
type SessionSerializer interface {
	Deserialize(d []byte, ss *sessions.Session) error
	Serialize(ss *sessions.Session) ([]byte, error)
}

// GobSerializer uses gob package to encode the session map
type GobSerializer struct{}

// Serialize using gob
func (s GobSerializer) Serialize(ss *sessions.Session) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(ss.Values)
	if err == nil {
		return buf.Bytes(), nil
	}
	return nil, err
}

type store struct {
	*RedisStore
}

// Deserialize back to map[interface{}]interface{}
func (s GobSerializer) Deserialize(d []byte, ss *sessions.Session) error {
	dec := gob.NewDecoder(bytes.NewBuffer(d))
	return dec.Decode(&ss.Values)
}

// Amount of time for cookies/redis keys to expire.
var sessionExpire = 86400 * 30

type RedisStore struct {
	RedisClient   redis.UniversalClient
	Options       *sessions.Options // default configuration
	Codecs        []securecookie.Codec
	keyPrefix     string
	serializer    SessionSerializer
	maxLength     int
	DefaultMaxAge int
}

func NewRedisStore(redisClient redis.UniversalClient, keyPairs ...[]byte) store {
	rs := &RedisStore{
		RedisClient: redisClient,
		Codecs:      securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: sessionExpire,
		},
		serializer:    GobSerializer{},
		maxLength:     4096,
		DefaultMaxAge: 60 * 20, // 20 minutes seems like a reasonable default
	}
	return store{rs}
}

// Get returns a session for the given name
// It returns a new session if there are no sessions  for the name.
func (rs *RedisStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return rs.New(r, name)
}
func (rs *RedisStore) New(r *http.Request, name string) (*sessions.Session, error) {
	var (
		err error
		ok  bool
	)
	session := sessions.NewSession(rs, name)
	options := *rs.Options
	session.Options = &options
	session.IsNew = true
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, rs.Codecs...)
		if err == nil {
			ok, err = rs.load(session)
			session.IsNew = !(err == nil && ok) // not new if no error and data available
		}
	}
	return session, err
}

func (rs *RedisStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Marked for deletion.
	if session.Options.MaxAge < 0 {
		if err := rs.delete(session); err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
	} else {
		// Build an alphanumeric key for the redis store.
		if session.ID == "" {
			session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
		}
		if err := rs.save(session); err != nil {
			return err
		}
		encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, rs.Codecs...)
		if err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	}
	return nil
}

// load reads the session from redis.
// returns true if there is a sessoin data in DB
func (rs *RedisStore) load(session *sessions.Session) (bool, error) {
	data, err := rs.RedisClient.Get(rs.keyPrefix + session.ID).Result()
	if err != nil {
		return false, err
	}
	return true, rs.serializer.Deserialize([]byte(data), session)
}

// delete removes keys from redis if MaxAge<0
func (rs *RedisStore) delete(session *sessions.Session) error {

	if _, err := rs.RedisClient.Del(rs.keyPrefix + session.ID).Result(); err != nil {
		return err
	}
	return nil
}

// save stores the session in redis.
func (rs *RedisStore) save(session *sessions.Session) error {
	b, err := rs.serializer.Serialize(session)
	if err != nil {
		return err
	}
	if rs.maxLength != 0 && len(b) > rs.maxLength {
		return errors.New("SessionStore: the value to store is too big")
	}

	age := session.Options.MaxAge
	if age == 0 {
		age = rs.DefaultMaxAge
	}
	_, err = rs.RedisClient.Set(rs.keyPrefix+session.ID, b, time.Duration(age)*time.Second).Result()
	return err
}
func (rs store) Options(op ginsessions.Options) {
	rs.RedisStore.Options = &sessions.Options{
		Path:     op.Path,
		Domain:   op.Domain,
		MaxAge:   op.MaxAge,
		Secure:   op.Secure,
		HttpOnly: op.HttpOnly,
	}
}
func (rs *RedisStore) SetMaxAge(v int) {
	var c *securecookie.SecureCookie
	var ok bool
	rs.Options.MaxAge = v
	for i := range rs.Codecs {
		if c, ok = rs.Codecs[i].(*securecookie.SecureCookie); ok {
			c.MaxAge(v)
		} else {
			fmt.Printf("Can't change MaxAge on codec %v\n", rs.Codecs[i])
		}
	}
}
