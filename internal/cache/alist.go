package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/synctv-org/synctv/internal/db"
	"github.com/synctv-org/synctv/internal/model"
	"github.com/synctv-org/synctv/internal/vendor"
	"github.com/synctv-org/vendors/api/alist"
	"github.com/zijiren233/gencontainer/refreshcache"
	"github.com/zijiren233/go-uhc"
)

type AlistUserCache = MapCache[*AlistUserCacheData, struct{}]

type AlistUserCacheData struct {
	Host     string
	ServerID string
	Token    string
	Backend  string
}

func NewAlistUserCache(userID string) *AlistUserCache {
	return newMapCache[*AlistUserCacheData, struct{}](func(ctx context.Context, key string, args ...struct{}) (*AlistUserCacheData, error) {
		return AlistAuthorizationCacheWithUserIDInitFunc(ctx, userID, key)
	}, 0)
}

func AlistAuthorizationCacheWithUserIDInitFunc(ctx context.Context, userID, serverID string) (*AlistUserCacheData, error) {
	v, err := db.GetAlistVendor(userID, serverID)
	if err != nil {
		return nil, err
	}
	return AlistAuthorizationCacheWithConfigInitFunc(ctx, v)
}

func AlistAuthorizationCacheWithConfigInitFunc(ctx context.Context, v *model.AlistVendor) (*AlistUserCacheData, error) {
	cli := vendor.LoadAlistClient(v.Backend)
	model.GenAlistServerID(v)

	if v.Username == "" {
		_, err := cli.Me(ctx, &alist.MeReq{
			Host: v.Host,
		})
		if err != nil {
			return nil, err
		}
		return &AlistUserCacheData{
			Host:     v.Host,
			ServerID: v.ServerID,
			Backend:  v.Backend,
		}, nil
	} else {
		resp, err := cli.Login(ctx, &alist.LoginReq{
			Host:     v.Host,
			Username: v.Username,
			Password: string(v.HashedPassword),
			Hashed:   true,
		})
		if err != nil {
			return nil, err
		}

		return &AlistUserCacheData{
			Host:     v.Host,
			ServerID: v.ServerID,
			Token:    resp.Token,
			Backend:  v.Backend,
		}, nil
	}
}

type AlistMovieCache = refreshcache.RefreshCache[*AlistMovieCacheData, *AlistMovieCacheFuncArgs]

func NewAlistMovieCache(movie *model.Movie) *AlistMovieCache {
	return refreshcache.NewRefreshCache(NewAlistMovieCacheInitFunc(movie), time.Minute*14)
}

type AlistProvider = string

const (
	AlistProviderAli = "AliyundriveOpen"
	AlistProvider115 = "115 Cloud"
)

type AlistMovieCacheData struct {
	URL      string
	Provider string
	Ali      *AlistAliCache
}

type AlistAliCache struct {
	M3U8ListFile []byte
	Subtitles    []*AliSubtitle
}

type AliSubtitle struct {
	Raw   *alist.FsOtherResp_VideoPreviewPlayInfo_LiveTranscodingSubtitleTaskList
	Cache *AliSubtitleCache
}

type AliSubtitleCache = refreshcache.RefreshCache[[]byte, struct{}]

func newAliSubtitlesCacheInitFunc(list []*alist.FsOtherResp_VideoPreviewPlayInfo_LiveTranscodingSubtitleTaskList) []*AliSubtitle {
	caches := make([]*AliSubtitle, len(list))
	for i, v := range list {
		if v.Status != "finished" {
			return nil
		}
		url := v.Url
		caches[i] = &AliSubtitle{
			Cache: refreshcache.NewRefreshCache(func(ctx context.Context, args ...struct{}) ([]byte, error) {
				r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				if err != nil {
					return nil, err
				}
				resp, err := uhc.Do(r)
				if err != nil {
					return nil, err
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return nil, fmt.Errorf("status code: %d", resp.StatusCode)
				}
				return io.ReadAll(resp.Body)
			}, 0),
			Raw: v,
		}
	}
	return caches
}

func genAliM3U8ListFile(urls []*alist.FsOtherResp_VideoPreviewPlayInfo_LiveTranscodingTaskList) []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("#EXTM3U\n")
	buf.WriteString("#EXT-X-VERSION:3\n")
	for _, v := range urls {
		if v.Status != "finished" {
			return nil
		}
		buf.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=\"%d\"\n", v.TemplateWidth*v.TemplateHeight, v.TemplateWidth, v.TemplateHeight, v.TemplateWidth))
		buf.WriteString(v.Url + "\n")
	}
	return buf.Bytes()
}

type AlistMovieCacheFuncArgs struct {
	UserCache *AlistUserCache
	UserAgent string
}

func NewAlistMovieCacheInitFunc(movie *model.Movie) func(ctx context.Context, args ...*AlistMovieCacheFuncArgs) (*AlistMovieCacheData, error) {
	return func(ctx context.Context, args ...*AlistMovieCacheFuncArgs) (*AlistMovieCacheData, error) {
		if len(args) == 0 {
			return nil, errors.New("need alist user cache")
		}
		userCache := args[0].UserCache
		if userCache == nil {
			return nil, errors.New("need alist user cache")
		}
		var (
			serverID string
			err      error
			truePath string
		)
		serverID, truePath, err = model.GetAlistServerIdFromPath(movie.Base.VendorInfo.Alist.Path)
		if err != nil {
			return nil, err
		}
		aucd, err := userCache.LoadOrStore(ctx, serverID)
		if err != nil {
			return nil, err
		}
		if aucd.Host == "" {
			return nil, errors.New("not bind alist vendor")
		}
		cli := vendor.LoadAlistClient(movie.Base.VendorInfo.Backend)
		fg, err := cli.FsGet(ctx, &alist.FsGetReq{
			Host:      aucd.Host,
			Token:     aucd.Token,
			Path:      truePath,
			Password:  movie.Base.VendorInfo.Alist.Password,
			UserAgent: args[0].UserAgent,
		})
		if err != nil {
			return nil, err
		}

		if fg.IsDir {
			return nil, fmt.Errorf("path is dir: %s", truePath)
		}

		cache := &AlistMovieCacheData{
			URL:      fg.RawUrl,
			Provider: fg.Provider,
		}
		if fg.Provider == AlistProviderAli {
			fo, err := cli.FsOther(ctx, &alist.FsOtherReq{
				Host:     aucd.Host,
				Token:    aucd.Token,
				Path:     truePath,
				Password: movie.Base.VendorInfo.Alist.Password,
				Method:   "video_preview",
			})
			if err != nil {
				return nil, err
			}
			cache.Ali = &AlistAliCache{
				M3U8ListFile: genAliM3U8ListFile(fo.VideoPreviewPlayInfo.LiveTranscodingTaskList),
				Subtitles:    newAliSubtitlesCacheInitFunc(fo.VideoPreviewPlayInfo.LiveTranscodingSubtitleTaskList),
			}
		}
		return cache, nil
	}
}
