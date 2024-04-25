package op

import (
	"errors"
	"sync"
	"time"

	"github.com/synctv-org/synctv/internal/db"
	"github.com/synctv-org/synctv/internal/model"
	"github.com/synctv-org/synctv/utils"
	"github.com/zijiren233/gencontainer/dllist"
	rtmps "github.com/zijiren233/livelib/server"
)

type movies struct {
	roomID string
	lock   sync.RWMutex
	list   dllist.Dllist[*Movie]
	once   sync.Once
}

func (m *movies) init() {
	m.once.Do(func() {
		for _, m2 := range db.GetAllMoviesByRoomID(m.roomID) {
			m.list.PushBack(&Movie{
				Movie: m2,
			})
		}
	})
}

func (m *movies) Len() int {
	m.init()
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.list.Len()
}

func (m *movies) AddMovie(mo *model.Movie) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()
	mo.Position = uint(time.Now().UnixMilli())
	movie := &Movie{
		Movie: mo,
	}

	err := movie.Validate()
	if err != nil {
		return err
	}

	err = db.CreateMovie(mo)
	if err != nil {
		return err
	}

	m.list.PushBack(movie)
	return nil
}

func (m *movies) AddMovies(mos []*model.Movie) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()
	inited := make([]*Movie, 0, len(mos))
	for _, mo := range mos {
		mo.Position = uint(time.Now().UnixMilli())
		movie := &Movie{
			Movie: mo,
		}

		err := movie.Validate()
		if err != nil {
			return err
		}

		inited = append(inited, movie)
	}

	err := db.CreateMovies(mos)
	if err != nil {
		return err
	}

	for _, mo := range inited {
		m.list.PushBack(mo)
	}

	return nil
}

func (m *movies) GetChannel(id string) (*rtmps.Channel, error) {
	if id == "" {
		return nil, errors.New("channel name is nil")
	}
	movie, err := m.GetMovieByID(id)
	if err != nil {
		return nil, err
	}
	return movie.Channel()
}

func (m *movies) Update(movieId string, movie *model.BaseMovie) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()
	for e := m.list.Front(); e != nil; e = e.Next() {
		if e.Value.Movie.ID == movieId {
			err := e.Value.Update(movie)
			if err != nil {
				return err
			}
			return db.SaveMovie(e.Value.Movie)
		}
	}
	return nil
}

func (m *movies) Clear() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	err := db.DeleteMoviesByRoomID(m.roomID)
	if err != nil {
		return err
	}
	for e := m.list.Front(); e != nil; e = e.Next() {
		_ = e.Value.Terminate()
	}
	m.list.Clear()
	return nil
}

func (m *movies) Close() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	for e := m.list.Front(); e != nil; e = e.Next() {
		_ = e.Value.Terminate()
	}
	m.list.Clear()
	return nil
}

func (m *movies) DeleteMovieByID(id string) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()

	err := db.DeleteMovieByID(m.roomID, id)
	if err != nil {
		return err
	}

	for e := m.list.Front(); e != nil; e = e.Next() {
		if e.Value.Movie.ID == id {
			_ = m.list.Remove(e).Terminate()
			return nil
		}
	}
	return errors.New("movie not found")
}

func (m *movies) DeleteMoviesByID(ids []string) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()

	err := db.DeleteMoviesByID(m.roomID, ids)
	if err != nil {
		return err
	}

	for _, id := range ids {
		for e := m.list.Front(); e != nil; e = e.Next() {
			if e.Value.Movie.ID == id {
				_ = m.list.Remove(e).Terminate()
				break
			}
		}
	}
	return nil
}

func (m *movies) GetMovieByID(id string) (*Movie, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.getMovieByID(id)
}

func (m *movies) getMovieByID(id string) (*Movie, error) {
	if id == "" {
		return nil, errors.New("movie id is nil")
	}
	m.init()
	for e := m.list.Front(); e != nil; e = e.Next() {
		if e.Value.Movie.ID == id {
			return e.Value, nil
		}
	}
	return nil, errors.New("movie not found")
}

func (m *movies) getMovieElementByID(id string) (*dllist.Element[*Movie], error) {
	m.init()
	for e := m.list.Front(); e != nil; e = e.Next() {
		if e.Value.Movie.ID == id {
			return e, nil
		}
	}
	return nil, errors.New("movie not found")
}

func (m *movies) SwapMoviePositions(id1, id2 string) error {
	m.init()
	m.lock.Lock()
	defer m.lock.Unlock()

	err := db.SwapMoviePositions(m.roomID, id1, id2)
	if err != nil {
		return err
	}

	movie1, err := m.getMovieElementByID(id1)
	if err != nil {
		return err
	}

	movie2, err := m.getMovieElementByID(id2)
	if err != nil {
		return err
	}

	movie1.Value.Movie.Position, movie2.Value.Movie.Position = movie2.Value.Movie.Position, movie1.Value.Movie.Position

	m.list.Swap(movie1, movie2)
	return nil
}

func (m *movies) GetMoviesWithPage(page, pageSize int, creator string) ([]*Movie, int) {
	m.init()
	m.lock.RLock()
	defer m.lock.RUnlock()

	var total int
	if creator != "" {
		for e := m.list.Front(); e != nil; e = e.Next() {
			if e.Value.Movie.CreatorID == creator {
				total++
			}
		}
	} else {
		total = m.list.Len()
	}

	start, end := utils.GetPageItemsRange(total, page, pageSize)
	ms := make([]*Movie, 0, end-start)
	i := 0
	for e := m.list.Front(); e != nil; e = e.Next() {
		if creator != "" && e.Value.Movie.CreatorID != creator {
			continue
		}
		if i >= start && i < end {
			ms = append(ms, e.Value)
		} else if i >= end {
			return ms, total
		}
		i++
	}
	return ms, total
}
