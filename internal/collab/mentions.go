package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type libraryMention struct {
	Reference  LibraryReference `json:"reference"`
	Title      string           `json:"title"`
	SpaceTitle string           `json:"spaceTitle"`
	Author     string           `json:"author"`
}

// Mentions reuse the library's resource identities and access rules, but include
// every published version. No selection, bundle, visit or model input is saved.
func (a *App) libraryMentions(ctx context.Context, query string) (any, error) {
	if utf8.RuneCountInString(query) > 256 {
		return nil, fmt.Errorf("搜索最多 256 字 / Search is limited to 256 characters")
	}
	terms := strings.Fields(strings.ToLower(query))
	a.mu.Lock()
	sessions := make([]*Session, 0, len(a.sessions))
	joined := make([]*Joined, 0, len(a.joined))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	for _, j := range a.joined {
		joined = append(joined, j)
	}
	a.mu.Unlock()
	views := make([]map[string]any, len(sessions)+len(joined))
	for i, s := range sessions {
		views[i] = s.view()
	}
	// Cached remote titles and annotations can outlive revoked access. Check each
	// peer afresh, with a shared deadline and bounded concurrency for typeahead.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	workers := make(chan struct{}, 4)
	for i, j := range joined {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case workers <- struct{}{}:
				defer func() { <-workers }()
			case <-ctx.Done():
				return
			}
			views[len(sessions)+i] = j.view(ctx)
		}()
	}
	wg.Wait()
	resources := []LibraryResource{}
	for _, view := range views {
		var space librarySpace
		data, _ := json.Marshal(view)
		if json.Unmarshal(data, &space) != nil || space.ID == "" || spaceAvailability(space) != "available" {
			continue
		}
		add := func(ref LibraryReference, searchText string) {
			r, err := resourceFor(space, ref)
			if err != nil || r.Availability != "available" {
				return
			}
			searchText = strings.ToLower(strings.Join([]string{r.Title, r.SpaceTitle, r.Author, searchText}, " "))
			for _, term := range terms {
				if !strings.Contains(searchText, term) {
					return
				}
			}
			resources = append(resources, r)
		}
		for _, material := range space.Materials {
			for _, version := range material.Versions {
				add(LibraryReference{SpaceID: space.ID, Kind: "material", MaterialID: material.ID, Version: version.Version}, fmt.Sprintf("material 材料 v%d version %d 版本%d 版本 %d", version.Version, version.Version, version.Version, version.Version))
			}
		}
		for _, note := range space.Annotations {
			add(LibraryReference{SpaceID: space.ID, Kind: "annotation", AnnotationID: note.ID}, "annotation 批注 "+note.Text)
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].UpdatedAt.Equal(resources[j].UpdatedAt) {
			return resources[i].Key < resources[j].Key
		}
		return resources[i].UpdatedAt.After(resources[j].UpdatedAt)
	})
	if len(resources) > 20 {
		resources = resources[:20]
	}
	items := make([]libraryMention, 0, len(resources))
	for _, r := range resources {
		items = append(items, libraryMention{Reference: r.Reference, Title: excerpt(r.Title, 120), SpaceTitle: excerpt(r.SpaceTitle, 100), Author: excerpt(r.Author, 60)})
	}
	_, language := a.UILanguage()
	return map[string]any{"resources": items, "language": language}, nil
}
