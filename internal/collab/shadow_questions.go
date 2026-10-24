package collab

import (
	"fmt"
	"net/http"
	"strconv"
)

func (a *App) shadowQuestionsHTTP(w http.ResponseWriter, r *http.Request, space string, detail bool) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	shadowID, id := q.Get("shadowId"), q.Get("id")
	if shadowID == "" || len(shadowID) > 256 || detail && (id == "" || len(id) > 256) {
		respond(w, nil, fmt.Errorf("需要准确的分身和提问身份"))
		return
	}
	_, engines, err := a.localAttention(r.Context(), space)
	if err != nil {
		respond(w, nil, err)
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	for _, e := range engines {
		ownID, _, _, _ := e.Identity()
		if ownID != shadowID {
			continue
		}
		if detail {
			out, err := e.QuestionDetail(id, q.Get("cursor"), limit)
			respond(w, out, err)
		} else {
			out, err := e.QuestionPage(q.Get("cursor"), q.Get("history") == "true", limit)
			respond(w, out, err)
		}
		return
	}
	respond(w, nil, fmt.Errorf("本机没有这位成员的对应分身"))
}
