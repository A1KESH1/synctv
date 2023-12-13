package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/synctv-org/synctv/internal/db"
	"github.com/synctv-org/synctv/internal/model"
	"github.com/synctv-org/synctv/internal/vendor"
	"github.com/synctv-org/synctv/utils"
	"github.com/synctv-org/vendors/api/bilibili"
	"github.com/zencoder/go-dash/v3/mpd"
	"github.com/zijiren233/gencontainer/refreshcache"
)

type BilibiliMpdCache struct {
	Mpd     string
	HevcMpd string
	Urls    []string
}

type BilibiliSubtitleCache map[string]*struct {
	Url string
	Srt *refreshcache.RefreshCache[[]byte]
}

func NewBilibiliSharedMpdCacheInitFunc(movie *model.Movie) func(ctx context.Context, args ...any) (*BilibiliMpdCache, error) {
	return func(ctx context.Context, args ...any) (*BilibiliMpdCache, error) {
		return BilibiliSharedMpdCacheInitFunc(ctx, movie, args...)
	}
}

func BilibiliSharedMpdCacheInitFunc(ctx context.Context, movie *model.Movie, args ...any) (*BilibiliMpdCache, error) {
	var cookies []*http.Cookie
	vendorInfo, err := db.GetBilibiliVendor(movie.CreatorID)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound("vendor")) {
			return nil, err
		}
	} else {
		cookies = utils.MapToHttpCookie(vendorInfo.Cookies)
	}
	cli := vendor.BilibiliClient(movie.Base.VendorInfo.Backend)
	var m, hevcM *mpd.MPD
	biliInfo := movie.Base.VendorInfo.Bilibili
	switch {
	case biliInfo.Epid != 0:
		resp, err := cli.GetDashPGCURL(ctx, &bilibili.GetDashPGCURLReq{
			Cookies: utils.HttpCookieToMap(cookies),
			Epid:    biliInfo.Epid,
		})
		if err != nil {
			return nil, err
		}
		m, err = mpd.ReadFromString(resp.Mpd)
		if err != nil {
			return nil, err
		}
		hevcM, err = mpd.ReadFromString(resp.HevcMpd)
		if err != nil {
			return nil, err
		}

	case biliInfo.Bvid != "":
		resp, err := cli.GetDashVideoURL(ctx, &bilibili.GetDashVideoURLReq{
			Cookies: utils.HttpCookieToMap(cookies),
			Bvid:    biliInfo.Bvid,
			Cid:     biliInfo.Cid,
		})
		if err != nil {
			return nil, err
		}
		m, err = mpd.ReadFromString(resp.Mpd)
		if err != nil {
			return nil, err
		}
		hevcM, err = mpd.ReadFromString(resp.HevcMpd)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("bvid and epid are empty")
	}
	m.BaseURL = append(m.BaseURL, fmt.Sprintf("/api/movie/proxy/%s/", movie.RoomID))
	id := 0
	movies := []string{}
	for _, p := range m.Periods {
		for _, as := range p.AdaptationSets {
			for _, r := range as.Representations {
				for i := range r.BaseURL {
					movies = append(movies, r.BaseURL[i])
					r.BaseURL[i] = fmt.Sprintf("%s?id=%d", movie.ID, id)
					id++
				}
			}
		}
	}
	for _, p := range hevcM.Periods {
		for _, as := range p.AdaptationSets {
			for _, r := range as.Representations {
				for i := range r.BaseURL {
					movies = append(movies, r.BaseURL[i])
					r.BaseURL[i] = fmt.Sprintf("%s?id=%d&t=hevc", movie.ID, id)
					id++
				}
			}
		}
	}
	s, err := m.WriteToString()
	if err != nil {
		return nil, err
	}
	s2, err := hevcM.WriteToString()
	if err != nil {
		return nil, err
	}
	return &BilibiliMpdCache{
		Urls:    movies,
		Mpd:     s,
		HevcMpd: s2,
	}, nil
}

func NewBilibiliNoSharedMovieCacheInitFunc(movie *model.Movie) func(ctx context.Context, id string, args ...any) (string, error) {
	return func(ctx context.Context, id string, args ...any) (string, error) {
		return BilibiliNoSharedMovieCacheInitFunc(ctx, id, movie, args...)
	}
}

func BilibiliNoSharedMovieCacheInitFunc(ctx context.Context, id string, movie *model.Movie, args ...any) (string, error) {
	var cookies []*http.Cookie
	vendorInfo, err := db.GetBilibiliVendor(id)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound("vendor")) {
			return "", err
		}
	} else {
		cookies = utils.MapToHttpCookie(vendorInfo.Cookies)
	}
	cli := vendor.BilibiliClient(movie.Base.VendorInfo.Backend)
	var u string
	biliInfo := movie.Base.VendorInfo.Bilibili
	switch {
	case biliInfo.Epid != 0:
		resp, err := cli.GetPGCURL(ctx, &bilibili.GetPGCURLReq{
			Cookies: utils.HttpCookieToMap(cookies),
			Epid:    biliInfo.Epid,
		})
		if err != nil {
			return "", err
		}
		u = resp.Url

	case biliInfo.Bvid != "":
		resp, err := cli.GetVideoURL(ctx, &bilibili.GetVideoURLReq{
			Cookies: utils.HttpCookieToMap(cookies),
			Bvid:    biliInfo.Bvid,
			Cid:     biliInfo.Cid,
		})
		if err != nil {
			return "", err
		}
		u = resp.Url

	default:
		return "", errors.New("bvid and epid are empty")

	}

	return u, nil
}

type bilibiliSubtitleResp struct {
	FontSize        float64 `json:"font_size"`
	FontColor       string  `json:"font_color"`
	BackgroundAlpha float64 `json:"background_alpha"`
	BackgroundColor string  `json:"background_color"`
	Stroke          string  `json:"Stroke"`
	Type            string  `json:"type"`
	Lang            string  `json:"lang"`
	Version         string  `json:"version"`
	Body            []struct {
		From     float64 `json:"from"`
		To       float64 `json:"to"`
		Sid      int     `json:"sid"`
		Location int     `json:"location"`
		Content  string  `json:"content"`
	} `json:"body"`
}

func NewBilibiliSubtitleCacheInitFunc(movie *model.Movie) func(ctx context.Context, args ...any) (BilibiliSubtitleCache, error) {
	return func(ctx context.Context, args ...any) (BilibiliSubtitleCache, error) {
		return BilibiliSubtitleCacheInitFunc(ctx, movie)
	}
}

func BilibiliSubtitleCacheInitFunc(ctx context.Context, movie *model.Movie, args ...any) (BilibiliSubtitleCache, error) {
	return initBilibiliSubtitleCache(ctx, movie, args...)
}

func initBilibiliSubtitleCache(ctx context.Context, movie *model.Movie, args ...any) (BilibiliSubtitleCache, error) {
	biliInfo := movie.Base.VendorInfo.Bilibili
	if biliInfo.Bvid == "" || biliInfo.Cid == 0 {
		return nil, errors.New("bvid or cid is empty")
	}

	var cookies []*http.Cookie
	vendorInfo, err := db.GetBilibiliVendor(movie.CreatorID)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound("vendor")) {
			return nil, err
		}
	} else {
		cookies = utils.MapToHttpCookie(vendorInfo.Cookies)
	}
	cli := vendor.BilibiliClient(movie.Base.VendorInfo.Backend)
	resp, err := cli.GetSubtitles(ctx, &bilibili.GetSubtitlesReq{
		Cookies: utils.HttpCookieToMap(cookies),
		Bvid:    biliInfo.Bvid,
		Cid:     biliInfo.Cid,
	})
	if err != nil {
		return nil, err
	}
	subtitleCache := make(BilibiliSubtitleCache, len(resp.Subtitles))
	for k, v := range resp.Subtitles {
		subtitleCache[k] = &struct {
			Url string
			Srt *refreshcache.RefreshCache[[]byte]
		}{
			Url: v,
			Srt: refreshcache.NewRefreshCache[[]byte](func(ctx context.Context, args ...any) ([]byte, error) {
				return translateBilibiliSubtitleToSrt(ctx, v)
			}, 0),
		}
	}

	return subtitleCache, nil
}

func convertToSRT(subtitles *bilibiliSubtitleResp) []byte {
	srt := bytes.NewBuffer(nil)
	counter := 0
	for _, subtitle := range subtitles.Body {
		srt.WriteString(
			fmt.Sprintf("%d\n%s --> %s\n%s\n\n",
				counter,
				formatTime(subtitle.From),
				formatTime(subtitle.To),
				subtitle.Content))
		counter++
	}
	return srt.Bytes()
}

func formatTime(seconds float64) string {
	hours := int(seconds) / 3600
	seconds = math.Mod(seconds, 3600)
	minutes := int(seconds) / 60
	seconds = math.Mod(seconds, 60)
	milliseconds := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, int(seconds), milliseconds)
}

func translateBilibiliSubtitleToSrt(ctx context.Context, url string) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https:%s", url), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", utils.UA)
	r.Header.Set("Referer", "https://www.bilibili.com")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var srt bilibiliSubtitleResp
	err = json.NewDecoder(resp.Body).Decode(&srt)
	if err != nil {
		return nil, err
	}
	return convertToSRT(&srt), nil
}

type BilibiliMovieCache struct {
	NoSharedMovie *MapCache[string]
	SharedMpd     *refreshcache.RefreshCache[*BilibiliMpdCache]
	Subtitle      *refreshcache.RefreshCache[BilibiliSubtitleCache]
}

func NewBilibiliMovieCache(movie *model.Movie) *BilibiliMovieCache {
	return &BilibiliMovieCache{
		NoSharedMovie: newMapCache[string](NewBilibiliNoSharedMovieCacheInitFunc(movie), time.Minute*115),
		SharedMpd:     refreshcache.NewRefreshCache[*BilibiliMpdCache](NewBilibiliSharedMpdCacheInitFunc(movie), time.Minute*115),
		Subtitle:      refreshcache.NewRefreshCache[BilibiliSubtitleCache](NewBilibiliSubtitleCacheInitFunc(movie), time.Minute*60),
	}
}
