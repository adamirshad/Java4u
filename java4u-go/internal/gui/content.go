package gui

// Live content for the home page: Mojang news, Modrinth mods and modpacks, and
// player avatars. Everything is fetched by Java4U (not the page) and cached briefly.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	newsURL       = "https://launchercontent.mojang.com/v2/news.json"
	newsImageBase = "https://launchercontent.mojang.com"
	modrinthAPI   = "https://api.modrinth.com/v2/search"
	profileAPI    = "https://sessionserver.mojang.com/session/minecraft/profile/"
	maxBody       = 8 << 20
)

type cacheEntry struct {
	at   time.Time
	data any
}

// contentCache remembers fetched results for a while, keyed by request.
type contentCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

func (c *contentCache) get(key string, ttl time.Duration) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || (ttl > 0 && time.Since(e.at) > ttl) {
		return nil, false
	}
	return e.data, true
}

func (c *contentCache) put(key string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cacheEntry{}
	}
	if len(c.m) > 200 { // keep it small
		c.m = map[string]cacheEntry{}
	}
	c.m[key] = cacheEntry{time.Now(), data}
}

func (s *Server) fetch(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// ------------------------------------------------------------ news

type newsItem struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Date  string `json:"date"`
	Image string `json:"image"`
	Thumb string `json:"thumb"`
	Link  string `json:"link"`
}

func (s *Server) handleNews(w http.ResponseWriter, r *http.Request) {
	if v, ok := s.cache.get("news", 30*time.Minute); ok {
		writeJSON(w, v)
		return
	}
	body, err := s.fetch(r.Context(), newsURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Could not load news: "+err.Error())
		return
	}
	var feed struct {
		Entries []struct {
			Title         string   `json:"title"`
			Text          string   `json:"text"`
			Date          string   `json:"date"`
			NewsType      []string `json:"newsType"`
			ReadMoreLink  string   `json:"readMoreLink"`
			PlayPageImage struct {
				URL string `json:"url"`
			} `json:"playPageImage"`
			NewsPageImage struct {
				URL string `json:"url"`
			} `json:"newsPageImage"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(body, &feed); err != nil {
		writeErr(w, http.StatusBadGateway, "Could not read news: "+err.Error())
		return
	}
	items := []newsItem{}
	for _, e := range feed.Entries {
		java := false
		for _, t := range e.NewsType {
			java = java || t == "Java"
		}
		if !java || !strings.HasPrefix(e.ReadMoreLink, "https://") {
			continue
		}
		items = append(items, newsItem{
			Title: e.Title,
			Text:  e.Text,
			Date:  e.Date,
			Image: imageURL(e.NewsPageImage.URL),
			Thumb: imageURL(e.PlayPageImage.URL),
			Link:  e.ReadMoreLink,
		})
		if len(items) == 8 {
			break
		}
	}
	s.cache.put("news", items)
	writeJSON(w, items)
}

func imageURL(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return ""
	}
	return newsImageBase + p
}

// ------------------------------------------------------------ Modrinth

type project struct {
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Icon        string `json:"icon"`
	Image       string `json:"image"`
	Downloads   int64  `json:"downloads"`
	URL         string `json:"url"`
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("type")
	if kind != "mod" && kind != "modpack" {
		writeErr(w, http.StatusBadRequest, "type must be mod or modpack")
		return
	}
	query := strings.TrimSpace(q.Get("q"))
	if len(query) > 100 {
		query = query[:100]
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	limit = min(max(limit, 1), 30)
	gameVersion := q.Get("version")
	if !versionRe.MatchString(gameVersion) {
		gameVersion = ""
	}

	key := fmt.Sprintf("browse|%s|%s|%d|%s", kind, query, limit, gameVersion)
	if v, ok := s.cache.get(key, 10*time.Minute); ok {
		writeJSON(w, v)
		return
	}

	facets := [][]string{{"project_type:" + kind}}
	if kind == "mod" {
		facets = append(facets, []string{"categories:fabric"}) // Chunky runs Fabric mods
	}
	if gameVersion != "" {
		facets = append(facets, []string{"versions:" + gameVersion})
	}
	facetJSON, _ := json.Marshal(facets)
	params := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"index":  {"downloads"},
		"facets": {string(facetJSON)},
	}
	if query != "" {
		params.Set("query", query)
		params.Set("index", "relevance")
	}
	body, err := s.fetch(r.Context(), modrinthAPI+"?"+params.Encode())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Could not reach Modrinth: "+err.Error())
		return
	}
	var res struct {
		Hits []struct {
			ProjectID       string   `json:"project_id"`
			Title           string   `json:"title"`
			Slug            string   `json:"slug"`
			Description     string   `json:"description"`
			Author          string   `json:"author"`
			IconURL         string   `json:"icon_url"`
			FeaturedGallery string   `json:"featured_gallery"`
			Gallery         []string `json:"gallery"`
			Downloads       int64    `json:"downloads"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		writeErr(w, http.StatusBadGateway, "Could not read Modrinth reply: "+err.Error())
		return
	}
	out := []project{}
	for _, h := range res.Hits {
		img := h.FeaturedGallery
		if img == "" && len(h.Gallery) > 0 {
			img = h.Gallery[0]
		}
		out = append(out, project{
			ProjectID:   h.ProjectID,
			Title:       h.Title,
			Slug:        h.Slug,
			Description: h.Description,
			Author:      h.Author,
			Icon:        modrinthImage(h.IconURL),
			Image:       modrinthImage(img),
			Downloads:   h.Downloads,
			URL:         "https://modrinth.com/" + kind + "/" + url.PathEscape(h.Slug),
		})
	}
	s.cache.put(key, out)
	writeJSON(w, out)
}

// modrinthImage keeps only images from Modrinth's CDN (the page's CSP allows no others).
func modrinthImage(u string) string {
	if strings.HasPrefix(u, "https://cdn.modrinth.com/") {
		return u
	}
	return ""
}

// ------------------------------------------------------------ avatars

var (
	uuidRe    = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	versionRe = regexp.MustCompile(`^[0-9A-Za-z.\-_ ]{0,32}$`)
)

// handleAvatar serves a player's face (with hat layer) from their Minecraft skin.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	id := strings.ReplaceAll(r.PathValue("uuid"), "-", "")
	if !uuidRe.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	key := "avatar|" + strings.ToLower(id)
	v, ok := s.cache.get(key, 6*time.Hour)
	if !ok {
		data, err := s.renderAvatar(r.Context(), id)
		if err != nil {
			http.Error(w, "no avatar", http.StatusNotFound)
			return
		}
		s.cache.put(key, data)
		v = data
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(v.([]byte))
}

func (s *Server) renderAvatar(ctx context.Context, id string) ([]byte, error) {
	body, err := s.fetch(ctx, profileAPI+id)
	if err != nil {
		return nil, err
	}
	var prof struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &prof); err != nil {
		return nil, err
	}
	skinURL := ""
	for _, p := range prof.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return nil, err
		}
		var tex struct {
			Textures struct {
				Skin struct {
					URL string `json:"url"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if err := json.Unmarshal(raw, &tex); err != nil {
			return nil, err
		}
		skinURL = tex.Textures.Skin.URL
	}
	u, err := url.Parse(skinURL)
	if err != nil || u.Host != "textures.minecraft.net" {
		return nil, errors.New("no skin")
	}
	u.Scheme = "https"
	skinData, err := s.fetch(ctx, u.String())
	if err != nil {
		return nil, err
	}
	skin, err := png.Decode(bytes.NewReader(skinData))
	if err != nil {
		return nil, err
	}
	if b := skin.Bounds(); b.Dx() < 64 || b.Dy() < 32 {
		return nil, errors.New("unexpected skin size")
	}

	// Face is at (8,8)-(16,16); the hat layer at (40,8)-(48,16). Scale 8x8 up to 64x64.
	const scale = 8
	face := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(face, face.Bounds(), skin, image.Pt(8, 8), draw.Src)
	draw.Draw(face, face.Bounds(), skin, image.Pt(40, 8), draw.Over)
	out := image.NewRGBA(image.Rect(0, 0, 8*scale, 8*scale))
	for y := 0; y < 8*scale; y++ {
		for x := 0; x < 8*scale; x++ {
			out.Set(x, y, face.At(x/scale, y/scale))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
