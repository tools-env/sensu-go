package routers

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"path"

	"github.com/gorilla/mux"
	"github.com/sensu/sensu-go/backend/apid/actions"
)

type errorBody struct {
	Error string `json:"error"`
	Code  uint32 `json:"code"`
}

func respondWith(w http.ResponseWriter, resources interface{}) {
	// Set content-type to JSON
	w.Header().Set("Content-Type", "application/json")

	// If not resource(s) are present return a 204 response code
	if resources == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	bytes, err := json.Marshal(resources)
	if err != nil {
		writeError(w, err)
		return
	}

	if _, err := w.Write(bytes); err != nil {
		logger.WithError(err).Error("failed to write response")
		writeError(w, err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	const fallback = `{"error": "failed to marshal error message"}`

	errBody := errorBody{}
	st := http.StatusInternalServerError

	// Wrap message in standard errorBody
	actionErr, ok := err.(actions.Error)
	if ok {
		errBody.Error = actionErr.Message
		errBody.Code = uint32(actionErr.Code)
		st = HTTPStatusFromCode(actionErr.Code)
	} else {
		errBody.Error = err.Error()
	}

	// Prevent browser from doing mime-sniffing
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Marshall error message to JSON
	errJSON, err := json.Marshal(errBody)
	if err != nil {
		logEntry := logger.WithField("errBody", errBody).WithError(err)
		logEntry.Error("failed to serialize error body")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := io.WriteString(w, fallback); err != nil {
			logEntry = logger.WithError(err)
			logEntry.Error("failed to write response")
		}
		return
	}

	// Write JSON
	w.WriteHeader(st)
	fmt.Println(w, errJSON)
}

// HTTPStatusFromCode returns http status code for given user action err code
func HTTPStatusFromCode(code actions.ErrCode) int {
	switch code {
	case actions.InternalErr:
		return http.StatusInternalServerError
	case actions.InvalidArgument:
		return http.StatusBadRequest
	case actions.NotFound:
		return http.StatusNotFound
	case actions.AlreadyExistsErr:
		return http.StatusConflict
	case actions.PermissionDenied:
		return http.StatusUnauthorized
	case actions.Unauthenticated:
		return http.StatusUnauthorized
	}

	logger.WithField("code", code).Errorf("unknown error code")
	return http.StatusInternalServerError
}

type actionHandlerFunc func(r *http.Request) (interface{}, error)

func actionHandler(action actionHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		records, err := action(r)
		if err != nil {
			writeError(w, err)
			return
		}

		respondWith(w, records)
	}
}

//
// resourceRoute mounts resources in a convetional RESTful manner.
//
//   routes := resourceRoute{pathPrefix: "checks", router: ...}
//   routes.index(myIndexAction)    // given action is mounted at GET /checks
//   routes.show(myShowAction)      // given action is mounted at GET /checks/:id
//   routes.update(myUpdateAction)  // given action is mounted at {PUT,PATCH} /checks/:id
//   routes.create(myCreateAction)  // given action is mounted at POST /checks
//   routes.destroy(myCreateAction) // given action is mounted at DELETE /checks/:id
//   routes.path("{id}/publish", publishAction).Methods(http.MethodDelete) // when you need something customer
//
type resourceRoute struct {
	router     *mux.Router
	pathPrefix string
}

func (r *resourceRoute) index(fn actionHandlerFunc) *mux.Route {
	return r.path("", fn).Methods(http.MethodGet)
}

func (r *resourceRoute) show(fn actionHandlerFunc) *mux.Route {
	return r.path("{id}", fn).Methods(http.MethodGet)
}

func (r *resourceRoute) create(fn actionHandlerFunc) *mux.Route {
	return r.path("", fn).Methods(http.MethodPost)
}

func (r *resourceRoute) update(fn actionHandlerFunc) *mux.Route {
	return r.path("{id}", fn).Methods(http.MethodPut, http.MethodPatch)
}

func (r *resourceRoute) destroy(fn actionHandlerFunc) *mux.Route {
	return r.path("{id}", fn).Methods(http.MethodDelete)
}

func (r *resourceRoute) path(p string, fn actionHandlerFunc) *mux.Route {
	fullPath := path.Join(r.pathPrefix, p)
	return handleAction(r.router, fullPath, fn)
}

func handleAction(router *mux.Router, path string, fn actionHandlerFunc) *mux.Route {
	logger.Errorf("HandleFunc %s", path)
	return router.HandleFunc(path, actionHandler(fn))
}

func unmarshalBody(req *http.Request, record interface{}) error {
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		logger.WithError(err).Error("unable to read request body")
		return err
	}
	defer req.Body.Close()

	// TODO: Support other types of requests other than JSON?
	err = json.Unmarshal(bodyBytes, record)
	if err != nil {
		logger.WithError(err).Error("unable to unmarshal request")
		return err
	}

	return nil
}
