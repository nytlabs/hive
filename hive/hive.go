// Hive is a platform for crowdsourcing apps.
// @APIVersion 1.0.0
// @Title Hive Crowdsourcing API
// @Description Hive is a platform for building crowdsourced applications on.
// @Contact jacqui.maher@nytimes.com
// @License Apache
// @LicenseUrl http://www.apache.org/licenses/

// @SubApi Operations about assets [/assets]
// @SubApi Operations about assignments [/assignments]
// @SubApi Operations about tasks [/tasks]
// @SubApi Operations about projects [/projects]
// @SubApi Operations about users [/users]

package hive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	elastigo "github.com/jacqui/elastigo/lib"
)

// Server runs the http service for hive's api
// It also stores some commonly accessed global settings
type Server struct {
	Port            string
	Index           string
	EsConn          elastigo.Conn
	ActiveProjectId string
}

// NewServer returns an instance of a Hive webserver that can be run (see main.go)
func NewServer() *Server {
	return &Server{}
}

// API metadata related to pagination
type meta struct {
	Total int
	From  int
	Size  int
}

// Counts are a map of category to total number of favorited assets, assignments overall, assignments by task.
// Examples:
//   User.Counts["Favorites"] = 4
//	 User.Counts["Assignments"] = 40
type Counts map[string]int

// SubmittedData is a map of task names to freeform json, used on Assignments and Assets
// On Assignments, this data is contributed by a single user. Assets only contain SubmittedData when
// the number of contributions reaches CompletionCriteria thresholds.
type SubmittedData map[string]interface{} // this is filled in once crowdsourcing success happens

type MetaProperty struct {
	Name string
	Type string
}

// Project is a single crowdsourcing app hosted in hive. Everything is scoped to a Project, at the very least.
// A project has Assets, Assignments, Tasks and Users.
type Project struct {
	Id              string // unique identifier suitable for friendly urls (slug)
	Name            string // a descriptive, displayable name or title
	Description     string // optional description, tagline, etc
	AssetCount      int    // calculated tally of assets
	TaskCount       int    // calculated tally of tasks
	UserCount       int    // calculated tally of users
	AssignmentCount Counts // calculated tally of assignments by state (finished, skipped, etc.)
	MetaProperties  []MetaProperty
}

// userFavorites are a map of asset IDs to asset records favorited by users.
// This is optional functionality you may use to list favorited items, for instance, on a user profile page.
// Ex: User.Favorites["WzSEohOLTV-e2pyHkHlHtg"] = Asset{Id: "WzSEohOLTV-e2pyHkHlHtg", ... }
type userFavorites map[string]Asset

// Users are the members of the crowd that you source in your app.
// They are scoped to a project, so the same person can have multiple records, one per project.
// Which fields are required is up to you - Hive will create a user with only an ID, to keep the barrier of entry
type User struct {
	Id             string // guid for the user in this project
	Name           string // person's name, could be a first + last, just first, a username, etc
	Email          string // email address is required
	Project        string // users are scoped to projects, the same person would have multiple user records across multiple projects
	ExternalId     string // you can optionally use some kind of external id to look up the user (ex: nytimes user id)
	Counts         Counts // calculation of favorites and assignments (total + by task) counts
	Favorites      userFavorites
	NewFavorites   userFavorites
	VerifiedAssets []string // list of verified asset ids that the user has contributed to
}

// Assignments are the work users have to do for a given task and asset.
// A user cannot get the same assignment twice: assignment scope is (Project + Task + Asset + User).
type Assignment struct {
	Id            string        // guid composed of ids from project + task + asset + user
	User          string        // the user doing this assignment
	Project       string        // the project
	Task          string        // the task
	Asset         Asset         // most importantly, what the user is completing a task on
	State         string        // assignments start out "unfinished" but can be "skipped" or "finished"
	SubmittedData SubmittedData // data the user submits when finishing the assignment
}

// Assets are what get assigned to users and can be images, pdfs, etc. All require a URL and are scoped to a project.
type Asset struct {
	Id            string                 // guid for the asset
	Project       string                 // assets are scoped to projects, the same asset in many projects would have multiple records
	Url           string                 // required, should be a direct link to the thing you want crowdsourced
	Name          string                 // optional, a displayable name
	Metadata      map[string]interface{} // optional, any additional info (ex: a newspaper issue date and page number)
	SubmittedData SubmittedData          // this is filled in once crowdsourcing success happens
	Favorited     bool
	Verified      bool
	Counts        Counts // calculation of favorites and assignments (total + by task) counts
}

type projectResponse struct {
	Project Project
}
type projectsResponse struct {
	Projects []Project
	Meta     meta
}

type assetResponse struct {
	Asset Asset
}
type favoriteResponse struct {
	AssetId string
	Action  string
}
type favoritesResponse struct {
	Favorites userFavorites
	Meta      meta
}
type assetsResponse struct {
	Assets []Asset
	Meta   meta
}

type taskResponse struct {
	Task Task
}
type tasksResponse struct {
	Tasks []Task
	Meta  meta
}

type userResponse struct {
	User User
}
type usersResponse struct {
	Users []User
	Meta  meta
}

type assignmentResponse struct {
	Assignment Assignment
}
type assignmentsResponse struct {
	Assignments []Assignment
	Meta        meta
}

/*
AssignmentCriteria determines when assets become eligible for assignment per task.

At a minimum, you should specify that the asset has not been verified for the next task by indicating that task name with empty data.
Here is an example criteria for a task "Categorize" that makes any asset not yet categorized eligible:

{
	"Categorize": {
		"SubmittedData": {}
	}
}

You can also set what data should have been submitted in another task before an asset is eligible for this one.
Example: a task "Categorize" that relies on assets completing a 'Find' task with specific data submitted:

{
	"Find": {
		"SubmittedData": {
			"category": "advertisement"
		}
	}
}
*/
type AssignmentCriteria struct {
	SubmittedData map[string]interface{}
}

// CompletionCriteria determines how an asset is verified.
// Set a minimum number of assignments along with a minimum number of matching assignments.
// All assignments must be finished to be counted here.
type CompletionCriteria struct {
	Total    int // minimum finished assigments
	Matching int // minimum assignments with the same answer
}

// Tasks are individual actions to do on an asset. A project can have one or more tasks.
// Criteria for assignment and verification of assets is stored on a task.
type Task struct {
	Id                 string             // guid, auto-generated
	Project            string             // tasks are scoped to projects
	Name               string             // a short sluggable name usable in urls (ex: find, transcribe, crop)
	Description        string             // a displayable title, description, instructions
	CurrentState       string             // is this task available, hidden, waiting or closed?
	AssignmentCriteria AssignmentCriteria // the criteria used when assigning valid assets for this task
	CompletionCriteria CompletionCriteria // the criteria used to mark an asset as 'completed' for this task
}

// FacetTerm maps Elasticsearch term + count from a faceted query.
type facetTerm struct {
	Term  string
	Count int
}

// FacetTerms is an array of faceted terms (term + count) along with the total.
type facetTerms struct {
	Terms []facetTerm
	Total int
}

// facetWrapper rolls up FacetTerms/FacetTerm to map the full faceted query results from Elasticsearch
// Note: Hive always uses the name 'Value' in faceted queries to fit into this struct.
type facetWrapper struct {
	Value facetTerms
}

type userBucket struct {
	Id    string `json:"key"`
	Count int    `json:"doc_count"`
}

type userBuckets struct {
	Buckets []userBucket `json:"buckets"`
}
type assetBucket struct {
	Id    string      `json:"key"`
	Count int         `json:"doc_count"`
	Users userBuckets `json:"users"`
}

type assetBuckets struct {
	Buckets []assetBucket `json:"buckets"`
}

type assetAgg struct {
	Assets assetBuckets `json:"assets"`
}

// wrapError is a convenience function to consistently format errors in json responses
func (s *Server) wrapError(err error) (formattedError []byte) {
	formattedError = []byte(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
	log.Println(string(formattedError))
	return formattedError
}

// wrapResponse is a convenience function to consistently format responses with the right headers
func (s *Server) wrapResponse(w http.ResponseWriter, r *http.Request, statusCode int, data []byte) {

	w.Header().Set("Content-Type", "application/json")

	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Host
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}

	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
	w.WriteHeader(statusCode)
	w.Write(data)
	// log.Println(string(data))
}

func defaultQuery(q url.Values, name string, defaultVal string) (val string) {
	qVal := q.Get(name)
	if qVal == "" {
		qVal = defaultVal
	}
	return qVal
}

// @Title AdminAssetHandler
// @Description retrieves a single project asset defined by an id
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   asset_id        path   string     true        "Retrieve asset with given ID only"
// @Success 200 {object}  Asset
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /admin/projects/{project_id}/assets/{asset_id} [get]
func (s *Server) AdminAssetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	assetId := vars["asset_id"]
	s.ActiveProjectId = vars["project_id"]

	asset, err := s.FindAsset(assetId)
	if err != nil {
		log.Println("failed finding asset", assetId, "because:", err)
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	assetWithCounts, err := s.CalculateAssetCounts(*asset)
	if err != nil {
		log.Println(err)
	}

	// format the json response
	resp := assetResponse{
		Asset: assetWithCounts,
	}

	assetJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assetJson)
}

// @Title AdminCreateAssetsHandler
// @Description creates assets in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   assets        body   string     true        "JSON-formatted array of assets, each requires a URL at minimum"
// @Success 200 {object}  assetsResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /admin/projects/{project_id}/assets [post]
func (s *Server) AdminCreateAssetsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	assets, err := s.CreateAssets(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	m := &meta{
		Total: len(assets),
		From:  0,
		Size:  10,
	}
	assetsJson, err := json.Marshal(&assetsResponse{
		Assets: assets,
		Meta:   *m,
	})
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assetsJson)
}

// @Title AdminAssetsHandler
// @Description returns a paginated list of assets in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   from        query   int     false        "If specified, will return a set of assets starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of assets specified as size"
// @Param   task        query   string     false        "If task is specified, will scope assets to those completed for the task 'task'"
// @Success 200 {object}  assetsResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /admin/projects/{project_id}/assets [get]
func (s *Server) AdminAssetsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	var assets []Asset
	var m meta
	var err error

	queryParams := r.URL.Query()
	p := Params{
		From:    defaultQuery(queryParams, "from", "0"),
		Size:    defaultQuery(queryParams, "size", "10"),
		Task:    defaultQuery(queryParams, "task", ""),
		State:   defaultQuery(queryParams, "state", ""),
		SortBy:  defaultQuery(queryParams, "sortBy", "Id"),
		SortDir: defaultQuery(queryParams, "sortDir", "asc"),
	}

	if p.State == "completed" {
		assets, m, err = s.FindAssetsWithDataForTask(p)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}
	}

	if p.State == "" {
		assets, m, err = s.FindAssets(p)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}
	}

	var assetsWithCounts []Asset
	for _, asset := range assets {
		//assetIds = append(assetIds, asset.Id)
		// nested aggregation of assignments completed on asset broken down by task then state
		// assetTmpl := `{ "query": { "bool": { "must": [ { "match_all": {} } ] } }, "aggs": { "assets": { "terms": { "field": "Asset.Id" }, "aggs": { "status_terms": { "terms": { "field": "State" } } } } } }`
		// aggregation of asset assignments by state
		// assetTmpl := `{ "query": { "bool": { "must": [ { "match_all": {} } ] } }, "aggs": { "assets": { "terms": { "field": "Asset.Id", "size": 20 }, "aggs": { "status_terms": { "terms": { "field": "State" } } } } } }`
		assetWithCounts, err := s.CalculateAssetCounts(asset)
		if err != nil {
			log.Println(err)
		}
		assetsWithCounts = append(assetsWithCounts, assetWithCounts)
	}
	//}

	assetsResponse := &assetsResponse{
		Assets: assetsWithCounts,
		Meta:   m,
	}
	// format the json response
	assetsJson, err := json.Marshal(assetsResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assetsJson)
}

// UpdateTaskState is called from disable and enable TaskHandlers
// It sets the current state of a task (available, waiting)
func (s *Server) UpdateTaskState(taskId string, state string) (task *Task, err error) {
	task, err = s.FindTask(taskId)
	if err != nil {
		return nil, err
	}
	task.CurrentState = state
	_, err = s.EsConn.Index(s.Index, "tasks", task.Id, nil, task)
	if err != nil {
		return nil, err
	}
	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return nil, err
	}
	return
}

// @Title DisableTaskHandler
// @Description makes a task unavailable for assignment by disabling it
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id        path   string     true        "Task ID"
// @Success 200 {object}  taskResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /admin/projects/{project_id}/tasks/{task_id}/disable [get]
func (s *Server) DisableTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]
	taskName := taskId
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskName = s.ActiveProjectId + "-" + taskName
	}

	task, err := s.UpdateTaskState(taskName, "waiting")
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	taskJson, err := json.Marshal(taskResponse{
		Task: *task,
	})

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, taskJson)
	return
}

// @Title EnableTaskHandler
// @Description makes a task available for assignment by enabling it
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id        path   string     true        "Task ID"
// @Success 200 {object}  taskResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /admin/projects/{project_id}/tasks/{task_id}/enable [get]
func (s *Server) EnableTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]
	taskName := taskId
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskName = s.ActiveProjectId + "-" + taskName
	}

	task, err := s.UpdateTaskState(taskName, "available")
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	taskJson, err := json.Marshal(taskResponse{
		Task: *task,
	})

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, taskJson)
	return
}

// @Title AdminTasksHandler
// @Description returns a paginated tasks in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   from        query   int     false        "If specified, will return a set of tasks starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of tasks specified as size"
// @Success 200 {object}  tasksResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /admin/projects/{project_id}/tasks [get]
func (s *Server) AdminTasksHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	queryParams := r.URL.Query()
	p := Params{
		From:    defaultQuery(queryParams, "from", "0"),
		Size:    defaultQuery(queryParams, "size", "10"),
		SortBy:  defaultQuery(queryParams, "sortBy", "Name"),
		SortDir: defaultQuery(queryParams, "sortDir", "asc"),
	}

	tasks, m, err := s.FindTasks(p)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	tasksResponse := &tasksResponse{
		Tasks: tasks,
		Meta:  m,
	}
	tasksJson, err := json.Marshal(tasksResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, tasksJson)
}

// @Title AdminCreateTasksHandler
// @Description creates or updates tasks in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   tasks        body   string     true        "JSON-formatted array of tasks to add or update"
// @Success 200 {object}  tasksResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /admin/projects/{project_id}/tasks [post]
func (s *Server) AdminCreateTasksHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	tasks, m, err := s.CreateTasks(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	tasksResponse := &tasksResponse{
		Tasks: tasks,
		Meta:  m,
	}
	tasksJson, err := json.Marshal(tasksResponse)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, tasksJson)

}

// @Title TasksHandler
// @Description returns a paginated tasks in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   from        query   int     false        "If specified, will return a set of tasks starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of tasks specified as size"
// @Success 200 {object}  tasksResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /projects/{project_id}/tasks [get]
func (s *Server) TasksHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	queryParams := r.URL.Query()
	p := Params{
		From:    defaultQuery(queryParams, "from", "0"),
		Size:    defaultQuery(queryParams, "size", "10"),
		SortBy:  defaultQuery(queryParams, "sortBy", "Name"),
		SortDir: defaultQuery(queryParams, "sortDir", "asc"),
	}
	tasks, m, err := s.FindTasks(p)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	tasksResponse := &tasksResponse{
		Tasks: tasks,
		Meta:  m,
	}
	tasksJson, err := json.Marshal(tasksResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, tasksJson)
}

// @Title AdminAssignmentsHandler
// @Description returns a paginated list of assignments in a task
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task        query   string     true        "Task ID"
// @Param   state        query   string     false        "Assignment state (unfinished, skipped, finished)"
// @Param   from        query   int     false        "If specified, will return a set of assignments starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of assignments specified as size"
// @Success 200 {object}  assignmentsResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assignments
// @Router /admin/projects/{project_id}/assignments [get]
func (s *Server) AdminAssignmentsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	queryParams := r.URL.Query()
	p := Params{
		From:    defaultQuery(queryParams, "from", "0"),
		Size:    defaultQuery(queryParams, "size", "10"),
		Task:    defaultQuery(queryParams, "task", ""),
		State:   defaultQuery(queryParams, "state", ""),
		SortBy:  defaultQuery(queryParams, "sortBy", "Id"),
		SortDir: defaultQuery(queryParams, "sortDir", "asc"),
	}

	assignments, m, err := s.FindAssignments(p)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	assignmentsResponse := &assignmentsResponse{
		Assignments: assignments,
		Meta:        m,
	}
	assignmentsJson, err := json.Marshal(assignmentsResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assignmentsJson)
}

// @Title AdminUserHandler
// @Description returns a single user in a project by ID
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   user_id        path   string     true        "User ID"
// @Success 200 {object}  userResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /users
// @Router /admin/projects/{project_id}/users/{user_id} [get]
func (s *Server) AdminUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	_, err := s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}
	user, err := s.FindUser(vars["user_id"])
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	if user.Counts["Assignments"] > 0 {
		var assetIds []string
		assetQuery := `{ "query": { "query_string": { "default_field": "Verified", "query": "true" } }, "aggs": { "assets": { "terms": { "field": "Id", "size": 0 } } } }`
		assetResults, _ := s.EsConn.Search(s.Index, "assets", nil, assetQuery)
		var a assetAgg
		_ = json.Unmarshal(assetResults.Aggregations, &a)

		for _, b := range a.Assets.Buckets {
			assetIds = append(assetIds, b.Id)
		}
		assetIdString := "\"" + strings.Join(assetIds, "\", \"") + "\""
		verifyQuery := fmt.Sprintf(`{"query": {"bool": {"must": [{"terms": {"assignments.Asset.Id": [%s]}},{"term": {"assignments.User": "%s" } } ], "must_not": [ { "term": { "assignments.State": "skipped" } }, { "term": { "assignments.State": "unfinished" } } ] } }, "from": 0, "size": %d}`, assetIdString, user.Id, user.Counts["Assignments"])
		verifyResults, _ := s.EsConn.Search(s.Index, "assignments", nil, verifyQuery)
		verifiedCount := verifyResults.Hits.Total
		user.Counts["VerifiedAssets"] = verifiedCount
		_, _ = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
	}
	userJson, err := json.Marshal(user)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	s.wrapResponse(w, r, 200, userJson)
}

// @Title AdminUsersHandler
// @Description returns a paginated list of users in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   from        query   int     false        "If specified, will return a set of users starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of users specified as size"
// @Success 200 {object}  usersResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /users
// @Router /admin/projects/{project_id}/users [get]
func (s *Server) AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	queryParams := r.URL.Query()
	p := Params{
		From:     defaultQuery(queryParams, "from", "0"),
		Size:     defaultQuery(queryParams, "size", "10"),
		Task:     defaultQuery(queryParams, "task", ""),
		State:    defaultQuery(queryParams, "state", ""),
		SortBy:   defaultQuery(queryParams, "sortBy", "Id"),
		SortDir:  defaultQuery(queryParams, "sortDir", "asc"),
		Verified: defaultQuery(queryParams, "verified", ""),
	}

	_, err := s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}
	users, m, err := s.FindUsers(p)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	var assetIds []string
	assetQuery := `{ "query": { "query_string": { "default_field": "Verified", "query": "true" } }, "aggs": { "assets": { "terms": { "field": "Id", "size": 0 } } } }`
	assetResults, _ := s.EsConn.Search(s.Index, "assets", nil, assetQuery)
	var a assetAgg
	_ = json.Unmarshal(assetResults.Aggregations, &a)

	for _, b := range a.Assets.Buckets {
		assetIds = append(assetIds, b.Id)
	}
	assetIdString := "\"" + strings.Join(assetIds, "\", \"") + "\""
	for _, user := range users {
		if user.Counts["Assignments"] > 0 {
			verifyQuery := fmt.Sprintf(`{"query": {"bool": {"must": [{"terms": {"assignments.Asset.Id": [%s]}},{"term": {"assignments.User": "%s" } } ], "must_not": [ { "term": { "assignments.State": "skipped" } }, { "term": { "assignments.State": "unfinished" } } ] } }, "from": 0, "size": %d}`, assetIdString, user.Id, user.Counts["Assignments"])
			verifyResults, _ := s.EsConn.Search(s.Index, "assignments", nil, verifyQuery)
			verifiedCount := verifyResults.Hits.Total
			user.Counts["VerifiedAssets"] = verifiedCount
			_, _ = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
		}
	}
	// format the json response
	usersResponse := &usersResponse{
		Users: users,
		Meta:  m,
	}
	usersJson, err := json.Marshal(usersResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, usersJson)
}

// Creates or updates a project by parsing the JSON body of the request.
func (s *Server) CreateProject(requestBody io.Reader) (project *Project, err error) {
	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &project)
	if err != nil {
		return nil, err
	}

	// store in elasticsearch
	_, err = s.EsConn.Index(s.Index, "projects", project.Id, nil, project)
	if err != nil {
		return nil, err
	}
	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return nil, err
	}

	return project, nil
}

// Creates or updates a task by parsing the JSON body of the request.
func (s *Server) CreateTask(requestBody io.Reader) (task *Task, err error) {
	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return
	}

	err = json.Unmarshal(body, &task)
	if err != nil {
		return
	}

	task.Id = strings.Join([]string{s.ActiveProjectId, strings.ToLower(task.Name)}, "-")
	if task.AssignmentCriteria.SubmittedData == nil {
		task.AssignmentCriteria.SubmittedData = make(map[string]interface{})
	}
	_, err = s.EsConn.Index(s.Index, "tasks", task.Id, nil, task)
	if err != nil {
		return
	}

	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}

	return task, nil
}

// Creates assets in this project by parsing the JSON body of the request.
func (s *Server) CreateAssets(requestBody io.Reader) (assets []Asset, err error) {
	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return assets, err
	}

	var importedJson struct {
		Assets []Asset
	}
	err = json.Unmarshal(body, &importedJson)
	if err != nil {
		return assets, err
	}

	assets, err = s.importAssets(importedJson.Assets)
	if err != nil {
		return assets, err
	}
	return assets, nil

}

// importAssets is a helper method called by CreateAssets that formats the request body appropriately for saving assets.
func (s *Server) importAssets(newAssets []Asset) (assets []Asset, err error) {
	p := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}
	tasks, _, err := s.FindTasks(p)
	if err != nil {
		return assets, err
	}

	submittedData := SubmittedData{}
	for _, task := range tasks {
		// submittedData[task.Name] = make(map[string]interface{})
		submittedData[task.Name] = nil
	}

	for _, asset := range newAssets {
		if len(asset.Url) == 0 {
			return assets, errors.New("Sorry, all assets must specify a url.")
		}
		asset.Project = s.ActiveProjectId
		asset.SubmittedData = submittedData
		asset.Counts = Counts{
			"Favorites":   0,
			"Assignments": 0,
			"finished":    0,
			"skipped":     0,
			"unfinished":  0,
		}

		// store in elasticsearch, which will generate a unique id
		result, err := s.EsConn.Index(s.Index, "assets", "", nil, asset)
		if err != nil {
			return assets, err
		}

		// get the id, store it in the asset source in elasticsearch
		asset.Id = result.Id
		_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
		if err != nil {
			return assets, err
		}

		if err == nil {
			assets = append(assets, asset)
		}
	}

	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}

	return assets, nil
}

// CreateTasks reads the request body POST'd to hive's admin to create/update tasks
func (s *Server) CreateTasks(requestBody io.Reader) (tasks []Task, m meta, err error) {
	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return
	}

	var importedJson struct {
		Tasks []Task
	}
	err = json.Unmarshal(body, &importedJson)
	if err != nil {
		return
	}

	tasks, m, err = s.importTasks(importedJson.Tasks)
	if err != nil {
		return
	}

	return tasks, m, nil
}

// importTasks is a helper method called by CreateTasks that formats the request body appropriately for saving tasks.
func (s *Server) importTasks(newTasks []Task) (tasks []Task, m meta, err error) {
	for _, task := range newTasks {
		if len(task.Name) == 0 {
			err = errors.New("Sorry, all tasks must specify a name.")
			return
		}
		task.Project = s.ActiveProjectId

		task.Id = strings.Join([]string{s.ActiveProjectId, strings.ToLower(task.Name)}, "-")
		if task.AssignmentCriteria.SubmittedData == nil {
			task.AssignmentCriteria.SubmittedData = make(map[string]interface{})
		}

		// store in elasticsearch, which will generate a unique id
		_, err := s.EsConn.Index(s.Index, "tasks", task.Id, nil, task)
		if err != nil {
			return tasks, m, err
		}
		tasks = append(tasks, task)
	}
	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}

	m.Total = len(tasks)
	m.From = 0
	m.Size = len(tasks)

	return tasks, m, nil
}

// CompleteTask uses the task's CompletionCriteria to find eligible assets for verification.
func (s *Server) CompleteTask(taskId string) ([]Asset, error) {
	var searchJson string
	var assets []Asset

	taskName := s.ActiveProjectId + "-" + taskId
	task, err := s.FindTask(taskName)
	if err != nil {
		return assets, err
	}

	query := `{
		"aggs": {
			"assets": {
				"terms": {
					"field": "Asset.Id",
					"size": 50000,
					"min_doc_count": %d
				},
				"aggs": {
					"users": {
						"terms": {
							"field": "User"
						}
					}
				}
			}
		},
		"query": {
			"filtered": {
				"filter": {
					"bool": {
						"must": [
						{
							"query": {
								"match": {
									"assignments.Task": "%s"
								}
							}
						},
						{
							"query": {
								"match": {
									"Project": "%s"
								}
							}
						},
						{
							"query": {
								"match": {
									"State": "finished"
								}
							}
						}
						]
					}
				}
			}
		}
	}`

	searchJson = fmt.Sprintf(query, task.CompletionCriteria.Total, taskName, s.ActiveProjectId)
	log.Println(searchJson)

	results, err := s.EsConn.Search(s.Index, "assignments", nil, searchJson)
	if err != nil {
		return assets, err
	}

	log.Println("** Assignments count:", results.Hits.Total)
	var a assetAgg
	err = json.Unmarshal(results.Aggregations, &a)
	if err != nil {
		return nil, err
	}

	/*
		assignments := make(map[string]Assignment)

		for _, hit := range results.Hits.Hits {
			var assignment Assignment
			rawMessage := hit.Source
			err = json.Unmarshal(*rawMessage, &assignment)
			if err != nil {
				continue
			}
			assignments[assignment.Asset.Id] = assignment
		}
	*/

	log.Println("** Assets Buckets:", len(a.Assets.Buckets))
	for _, b := range a.Assets.Buckets {
		if b.Count >= task.CompletionCriteria.Matching {
			log.Println("Completing asset", b.Id, "for task", task.Name)

			assignmentQuery := `{
				"query": {
					"filtered": {
						"filter": {
							"bool": {
								"must": [
								{
									"query": {
										"match": {
											"Task": "%s"
										}
									}
								},
								{
									"query": {
										"match": {
											"Asset.Id": "%s"
										}
									}
								},
								{
									"query": {
										"match": {
											"Project": "%s"
										}
									}
								},
								{
									"query": {
										"match": {
											"State": "finished"
										}
									}
								}
								]
							}
						}
					}
				}
			}`
			assignmentSearchJson := fmt.Sprintf(assignmentQuery, taskName, b.Id, s.ActiveProjectId)
			log.Println(assignmentSearchJson)
			assignmentResults, err := s.EsConn.Search(s.Index, "assignments", nil, assignmentSearchJson)
			if err != nil {
				log.Println("error searching for matching assignment:", err)
				return nil, err
			}
			log.Println("** Matching assignments count:", assignmentResults.Hits.Total)

			var matchingAssignments []Assignment
			var sdTrackers []SubmittedDataTracker
			for _, assignmentHit := range assignmentResults.Hits.Hits {
				var matchingAssignment Assignment
				rawMessage := assignmentHit.Source
				err = json.Unmarshal(*rawMessage, &matchingAssignment)
				if err != nil {
					log.Println(err)
					continue
				}

				sdTrackers = collateSubmittedData(sdTrackers, matchingAssignment.SubmittedData)
				matchingAssignments = append(matchingAssignments, matchingAssignment)
			}

			log.Println("sdTrackers:", sdTrackers)
			for _, tracker := range sdTrackers {
				if tracker.Count >= task.CompletionCriteria.Matching {
					log.Println("found", tracker.Count, "matching sds!")
					asset, err := s.CompleteAsset(b.Id, *task, tracker.Value)
					if err != nil {
						log.Println("error completing asset", err)
						continue
					}
					assets = append(assets, *asset)
					for _, a := range matchingAssignments {
						a.State = "verified"
						log.Println("verifying assignment", a.Id)
						_, err = s.EsConn.Index(s.Index, "assignments", a.Id, nil, a)
						if err != nil {
							log.Println("error saving assignment record:", err)
						}
					}
					continue
				}
			}
		}
	}

	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return assets, err
	}

	return assets, err
}

type SubmittedDataTracker struct {
	Value SubmittedData
	Count int
}

func collateSubmittedData(sdt []SubmittedDataTracker, item SubmittedData) []SubmittedDataTracker {
	log.Println("---------------------------------------")
	log.Println("sdt size:", len(sdt))
	log.Println("sdt before:", sdt)
	log.Println("item:", item)
	foundIt := false
	for i, tracker := range sdt {
		if reflect.DeepEqual(tracker.Value, item) {
			log.Println("found a match")
			// we've seen this before
			tracker.Count += 1
			sdt[i] = tracker
			log.Println("count is now:", tracker.Count)
			foundIt = true
		}
	}
	log.Println("sdt after:", sdt)
	if !foundIt {
		log.Println("didn't find it")
		sdt = append(sdt, SubmittedDataTracker{
			Value: item,
			Count: 1,
		})
	}
	log.Println("---------------------------------------")
	return sdt
}

func appendIfMissing(slice []string, item string) []string {
	for _, ele := range slice {
		if ele == item {
			return slice
		}
	}
	return append(slice, item)
}

// CompleteAsset is called by CompleteTask to store verified submitted data on assets.
func (s *Server) CompleteAsset(assetId string, task Task, submittedData map[string]interface{}) (*Asset, error) {
	asset, err := s.FindAsset(assetId)
	if err != nil {
		return asset, err
	}
	if asset == nil {
		assetError := errors.New("Failed finding an asset with that id.")
		return asset, assetError
	}
	asset.SubmittedData[task.Name] = submittedData
	p := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, _, err := s.FindTasks(p)
	if err != nil {
		return asset, err
	}
	assetVerified := true
	for _, t := range tasks {
		if asset.SubmittedData[t.Name] == nil {
			assetVerified = false
		}
	}
	if assetVerified {
		log.Println("Asset #", asset.Id, "is considered verified!")
	}
	asset.Verified = assetVerified
	_, err = s.EsConn.Index(s.Index, "assets", assetId, nil, asset)
	if err != nil {
		return asset, err
	}
	return asset, nil
}

// CalculateAssetCounts tallies up number of assignments, favorites, etc an asset has and saves it
func (s *Server) CalculateAssetCounts(asset Asset) (Asset, error) {
	assetTmpl := `{
		"query": {
			"bool": {
				"must": [
				{
					"term": {
						"assignments.Asset.Id": "%s"
					}
				}
				],
				"must_not": [],
				"should": []
			}
		},
		"from": 0,
		"size": 10,
		"sort": [],
		"facets": {
			"Value": {
				"terms": {
					"field": "State"
				}
			}
		}
	}`
	assignmentQuery := fmt.Sprintf(assetTmpl, asset.Id)
	assignResults, err := s.EsConn.Search(s.Index, "assignments", nil, assignmentQuery)
	if err != nil {
		return asset, err
	}
	var a facetWrapper
	err = json.Unmarshal(assignResults.Facets, &a)
	if err != nil {
		return asset, err
	}

	if len(asset.Counts) <= 0 {
		asset.Counts = Counts{
			"Favorites":   0,
			"Assignments": 0,
			"finished":    0,
			"skipped":     0,
			"unfinished":  0,
		}
	} else {
		asset.Counts = Counts{
			"Assignments": 0,
			"finished":    0,
			"skipped":     0,
			"unfinished":  0,
		}
	}
	asset.Counts["Assignments"] = a.Value.Total
	for _, facetTerm := range a.Value.Terms {
		asset.Counts[facetTerm.Term] = facetTerm.Count
	}

	_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
	if err != nil {
		return asset, err
	}
	return asset, nil
}

func (s *Server) UpdateAssignment(requestBody io.Reader) (assignment *Assignment, err error) {
	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &assignment)
	if err != nil {
		return nil, err
	}

	//assignment.State = "finished"

	asset, _ := s.FindAsset(assignment.Asset.Id)
	if asset != nil {
		// Set counts on asset
		if len(asset.Counts) <= 0 {
			asset.Counts = Counts{
				"Favorites":   0,
				"Assignments": 1,
				"finished":    0,
				"skipped":     0,
				"unfinished":  1,
			}
		}

		asset.Counts[assignment.State] += 1
		asset.Counts["unfinished"] -= 1

		_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
		if err != nil {
			return nil, err
		}
		// ensure the asset is updated on the assignment record
		assignment.Asset = *asset
	}

	_, err = s.EsConn.Index(s.Index, "assignments", assignment.Id, nil, assignment)
	if err != nil {
		return nil, err
	}
	// refresh the index, attempting to fix "skipped" assignment issue #4
	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return nil, err
	}

	// add finished assignments to the user's list
	if assignment.State == "finished" {
		user, err := s.FindUser(assignment.User)
		if err != nil {
			return nil, err
		}
		user.Counts["Assignments"]++
		user.Counts[assignment.Task]++

		p := Params{
			From:    "0",
			Size:    "10",
			SortBy:  "Name",
			SortDir: "asc",
		}

		tasks, _, err := s.FindTasks(p)
		if err != nil {
			for _, task := range tasks {
				// Set any missing task counts to zero
				_, ok := user.Counts[task.Id]
				if !ok {
					user.Counts[task.Id] = 0
				}
			}
		}

		_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
		if err != nil {
			return nil, err
		}
	}
	return assignment, nil
}

// CreateAssetAssignment is called by the AssignAssetHandler to generate a new assignment for a particular asset, task and user
func (s *Server) CreateAssetAssignment(taskId string, userId string, assetId string) (assignment *Assignment, err error) {
	user, _ := s.FindUser(userId)
	if user == nil {
		tmpUser, err := s.CreateUserFromMissingCookieValue(userId)
		if err != nil {
			userError := errors.New("Assignments can't be created without a user: failed creating a new anon user")
			return nil, userError
		}
		user = &tmpUser
	}

	asset, err := s.FindAsset(assetId)
	if asset == nil {
		assetError := errors.New("Failed finding an asset with that id.")
		return nil, assetError
	}

	// Set counts on asset
	if len(asset.Counts) <= 0 {
		asset.Counts = Counts{
			"Favorites":   0,
			"Assignments": 0,
			"finished":    0,
			"skipped":     0,
			"unfinished":  0,
		}
	}
	asset.Counts["Assignments"] += 1
	asset.Counts["unfinished"] += 1
	_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
	if err != nil {
		log.Println(err)
	}

	assignmentId := strings.Join([]string{s.ActiveProjectId, taskId, assetId, userId}, "HIVE")
	assignment = &Assignment{
		Id:      assignmentId,
		User:    userId,
		Project: s.ActiveProjectId,
		Task:    taskId,
		Asset:   *asset,
		State:   "unfinished",
	}

	_, err = s.EsConn.Index(s.Index, "assignments", assignment.Id, nil, assignment)
	if err != nil {
		return nil, err
	}
	return assignment, nil
}

// CreateAssignment is called by the userAssignmentHandler to generate an assignment for the given user and task,
// picking an eligible asset for that task and user.
func (s *Server) CreateAssignment(taskId string, userId string) (assignment *Assignment, err error) {

	user, _ := s.FindUser(userId)
	if user == nil {
		tmpUser, err := s.CreateUserFromMissingCookieValue(userId)
		if err != nil {
			userError := errors.New("Assignments can't be created without a user: failed creating a new anon user")
			return nil, userError
		}
		user = &tmpUser
	}

	task, err := s.FindTask(taskId)
	if err != nil {
		return nil, err
	}

	if task.CurrentState != "available" {
		taskError := errors.New("Invalid task")
		return nil, taskError
	}

	searchQuery := `{
  "query": {
    "bool": {
      "must": [
        {
          "term": {
            "assignments.Project": "%s"
          }
        },
        {
          "term": {
            "assignments.Task": "%s"
          }
        },
        {
          "term": {
            "assignments.User": "%s"
          }
        },
        {
          "term": {
            "assignments.State": "unfinished"
          }
        }
      ]
    }
  }
}`

	searchJson := fmt.Sprintf(searchQuery, s.ActiveProjectId, taskId, userId)

	results, err := s.EsConn.Search(s.Index, "assignments", nil, searchJson)
	if err != nil {
		return nil, err
	}

	// found an unfinished assignment
	if results.Hits.Total > 0 {
		err = json.Unmarshal(*results.Hits.Hits[0].Source, &assignment)
		if err != nil {
			return nil, err
		}
		return assignment, nil

		// create a new assignment
	} else {
		assignmentAsset, err := s.FindAssignmentAsset(*task, *user)
		if err != nil {
			return nil, err
		}

		// Set counts on asset
		if len(assignmentAsset.Counts) <= 0 {
			assignmentAsset.Counts = Counts{
				"Favorites":   0,
				"Assignments": 0,
				"finished":    0,
				"skipped":     0,
				"unfinished":  0,
			}
		}

		// Since this asset is being assigned now, update the total assignments count
		assignmentAsset.Counts["Assignments"] += 1

		// And update the unfinished count, since it's a new assignment
		assignmentAsset.Counts["unfinished"] += 1

		_, err = s.EsConn.Index(s.Index, "assets", assignmentAsset.Id, nil, assignmentAsset)
		if err != nil {
			return nil, err
		}

		assignmentId := strings.Join([]string{s.ActiveProjectId, taskId, assignmentAsset.Id, user.Id}, "HIVE")
		assignment = &Assignment{
			Id:      assignmentId,
			User:    userId,
			Project: s.ActiveProjectId,
			Task:    taskId,
			Asset:   assignmentAsset,
			State:   "unfinished",
		}

		_, err = s.EsConn.Index(s.Index, "assignments", assignment.Id, nil, assignment)
		if err != nil {
			return nil, err
		}
		return assignment, nil
	}
}

// Count composes a simple elasticsearch query scoping results to the current project, returning a total of 'countWhat'
// This method is used to tally number of tasks and assets for instance.
func (s *Server) Count(countWhat string) (count int, err error) {
	var args map[string]interface{}

	projectQuery := fmt.Sprintf(`{ "query": { "term" : {"Project": "%s" } } }`, s.ActiveProjectId)
	countResponse, err := s.EsConn.Count(s.Index, countWhat, args, projectQuery)
	if err != nil {
		return
	}
	count = countResponse.Count
	return
}

// CountAssignments returns a map of assignment states to totals for each scoped to the current project.
func (s *Server) CountAssignments() (assignmentCount map[string]int, err error) {
	projectQuery := fmt.Sprintf(`{
		"facets": {
			"Value": {
				"terms": {
					"field": "State"
				}
			}
		},
		"query": {
			"filtered": {
				"filter": {
					"bool": {
						"must": [
						{
							"query": {
								"match": {
									"Project": "%s"
								}
							}
						}
						]
					}
				}
			}
		}
	}`, s.ActiveProjectId)
	results, err := s.EsConn.Search(s.Index, "assignments", nil, projectQuery)
	if err != nil {
		return
	}
	var a facetWrapper
	err = json.Unmarshal(results.Facets, &a)
	if err != nil {
		return nil, err
	}

	assignmentCount = make(map[string]int)
	for _, t := range a.Value.Terms {
		assignmentCount[strings.Title(t.Term)] = t.Count
	}
	assignmentCount["Total"] = a.Value.Total
	return assignmentCount, nil
}

// FindProject looks up a project by id, tallying counts of assets, users, tasks and assignments.
func (s *Server) FindProject(id string) (project *Project, err error) {
	err = s.EsConn.GetSource(s.Index, "projects", id, nil, &project)
	if err != nil {
		return nil, err
	}
	project.AssetCount, _ = s.Count("assets")
	project.UserCount, _ = s.Count("users")
	project.TaskCount, _ = s.Count("tasks")

	project.AssignmentCount, _ = s.CountAssignments()

	return project, nil
}

// FindProjects returns all projects, tallying counts of assets, users, tasks and assignments for each.
func (s *Server) FindProjects(p Params) (projects []Project, m meta, err error) {
	query := elastigo.Search(s.Index).Type("projects").From(p.From).Size(p.Size)
	results, err := query.Result(&s.EsConn)

	if err != nil {
		return
	}

	resultCount := results.Hits.Total

	m.Total = resultCount
	m.From, _ = strconv.Atoi(p.From)
	m.Size, _ = strconv.Atoi(p.Size)
	if resultCount <= 0 {
		err = errors.New("No projects found")
		return

	} else {
		for _, hit := range results.Hits.Hits {
			var project Project
			rawMessage := hit.Source
			err = json.Unmarshal(*rawMessage, &project)
			if err != nil {
				return
			}
			project.AssetCount, _ = s.Count("assets")
			project.UserCount, _ = s.Count("users")
			project.TaskCount, _ = s.Count("tasks")
			project.AssignmentCount, _ = s.CountAssignments()

			projects = append(projects, project)
		}
	}
	return
}

// FindUser looks up a user by id. If a matching user isn't found, it will create a new user and return it.
// TODO: make the CreateUser part optional/conditional?
func (s *Server) FindUser(id string) (user *User, err error) {
	if id == "" {
		userData := strings.NewReader(fmt.Sprintf(`{"Project": "%s"}`, s.ActiveProjectId))
		user, err = s.CreateUser(userData)
		if err != nil {
			return nil, err
		}
		return user, nil
	}

	err = s.EsConn.GetSource(s.Index, "users", id, nil, &user)

	if err != nil {
		var args map[string]interface{}
		userExists, _ := s.EsConn.ExistsBool(s.Index, "users", id, args)
		if !userExists {
			return nil, nil
		}
		return nil, err
	}

	p := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, _, err := s.FindTasks(p)
	if err == nil {
		for _, task := range tasks {
			_, ok := user.Counts[task.Id]
			if !ok {
				user.Counts[task.Id] = 0
			}
		}
	}
	return user, nil
}

// FindTask looks up a task by id
func (s *Server) FindTask(id string) (task *Task, err error) {
	err = s.EsConn.GetSource(s.Index, "tasks", id, nil, &task)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// FindTasks returns an array of tasks for the current project
func (s *Server) FindTasks(p Params) (tasks []Task, m meta, err error) {
	query := elastigo.Search(s.Index).Type("tasks").Filter(
		elastigo.Filter().Terms("Project", s.ActiveProjectId),
	).From(p.From).Size(p.Size)
	if p.SortDir == "desc" {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Desc(),
		)
	} else {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Asc(),
		)
	}
	results, err := query.Result(&s.EsConn)

	if err != nil {
		tasks = make([]Task, 0)
		return
	}

	for _, hit := range results.Hits.Hits {
		var task Task
		rawMessage := hit.Source
		err = json.Unmarshal(*rawMessage, &task)
		if err != nil {
			return
		}
		tasks = append(tasks, task)
	}
	return
}

// FindUsers returns an array of users in the current project, along with pagination meta information
// 'from' and 'size' parameters determine the offset and limit passed to the database.
func (s *Server) FindUsers(p Params) (users []User, m meta, err error) {
	query := elastigo.Search(s.Index).Type("users").Filter(
		elastigo.Filter().Terms("Project", s.ActiveProjectId),
	).From(p.From).Size(p.Size)
	if p.SortDir == "desc" {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Desc(),
		)
	} else {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Asc(),
		)
	}

	results, err := query.Result(&s.EsConn)

	if err != nil {
		users = make([]User, 0)
		return users, m, nil
	}

	resultCount := results.Hits.Total

	m.Total = resultCount
	m.From, _ = strconv.Atoi(p.From)
	m.Size, _ = strconv.Atoi(p.Size)

	taskParams := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, m, err := s.FindTasks(taskParams)
	for _, hit := range results.Hits.Hits {
		var user User
		rawMessage := hit.Source
		err = json.Unmarshal(*rawMessage, &user)

		if err != nil {
			err = nil
		}
		if len(tasks) > 0 {
			for _, task := range tasks {
				_, ok := user.Counts[task.Id]
				if !ok {
					user.Counts[task.Id] = 0
				}
			}
		}
		users = append(users, user)
	}
	return
}

// FindAsset looks up an asset by id.
func (s *Server) FindAsset(id string) (asset *Asset, err error) {
	err = s.EsConn.GetSource(s.Index, "assets", id, nil, &asset)
	if err != nil {
		return nil, err
	}
	return asset, nil
}

type Params struct {
	From     string
	Size     string
	SortBy   string
	SortDir  string
	Task     string
	State    string
	Verified string
}

// FindAssets returns an array of assets in the current project, along with pagination meta information.
// 'from' and 'size' parameters determine the offset and limit passed to the database.
func (s *Server) FindAssets(p Params) (assets []Asset, m meta, err error) {
	query := elastigo.Search(s.Index).Type("assets").Filter(
		elastigo.Filter().Terms("Project", s.ActiveProjectId),
	).From(p.From).Size(p.Size)
	if p.SortDir == "desc" {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Desc(),
		)
	} else {
		query = query.Sort(
			elastigo.Sort(p.SortBy).Asc(),
		)
	}
	results, err := query.Result(&s.EsConn)

	if err != nil {
		return
	}

	resultCount := results.Hits.Total

	m.Total = resultCount
	m.From, _ = strconv.Atoi(p.From)
	m.Size, _ = strconv.Atoi(p.Size)

	for _, hit := range results.Hits.Hits {
		var asset Asset
		rawMessage := hit.Source
		err = json.Unmarshal(*rawMessage, &asset)
		if err != nil {
			return
		}
		/*
			// use this when reindexing assets
					_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
					if err != nil {
						return
					}
		*/
		if len(asset.Counts) <= 0 {
			asset.Counts = Counts{
				"Favorites":   0,
				"Assignments": 0,
				"finished":    0,
				"skipped":     0,
				"unfinished":  0,
			}
		}
		assets = append(assets, asset)
	}
	/*
		// use this when reindexing assets
		_, err = s.EsConn.Refresh(s.Index)
		if err != nil {
			return
		}
	*/
	return
}

// FindAssignments returns an array of assignments in the current project, given task and state, along with pagination meta information.
// 'from' and 'size' parameters determine the offset and limit passed to the database.
func (s *Server) FindAssignments(p Params) (assignments []Assignment, m meta, err error) {
	_, err = s.EsConn.Refresh(s.Index)
	if err != nil {
		return
	}

	if !strings.HasPrefix(p.Task, s.ActiveProjectId) && p.Task != "" {
		p.Task = s.ActiveProjectId + "-" + p.Task
	}

	musts := []string{}
	musts = append(musts, fmt.Sprintf(` { "query": { "match": { "Project": "%s" } } }`, s.ActiveProjectId))

	if p.Task != "" {
		musts = append(musts, fmt.Sprintf(`{ "query": { "match": { "Task": "%s" } } }`, p.Task))
	}

	if p.State != "" {
		musts = append(musts, fmt.Sprintf(` { "query": { "match": { "State": "%s" } } }`, p.State))
	}

	searchQuery := `{
		"query": {
			"filtered": {
				"filter": {
					"bool": {
						"must": [%s ]
					}
				}
			}
		},
		"from": %s,
		"size": %s,
		"sort": [ { "%s": { "order" : "%s" } } ]
	}`

	searchJson := fmt.Sprintf(searchQuery, strings.Join(musts, ", "), p.From, p.Size, p.SortBy, p.SortDir)
	results, err := s.EsConn.Search(s.Index, "assignments", nil, searchJson)
	if err != nil {
		return
	}

	m.Total = results.Hits.Total
	m.From, _ = strconv.Atoi(p.From)
	m.Size, _ = strconv.Atoi(p.Size)

	for _, hit := range results.Hits.Hits {
		var assignment Assignment
		rawMessage := hit.Source
		err = json.Unmarshal(*rawMessage, &assignment)
		if err != nil {
			return
		}
		assignments = append(assignments, assignment)
	}
	if len(assignments) <= 0 {
		assignments = make([]Assignment, 0)
	}
	return
}

// FindAssetsWithDataForTask returns a list of assets in the current project and given task with submitted/verified data,
// along with pagination meta information.
// 'from' and 'size' parameters determine the offset and limit passed to the database.
// 'sortBy' and 'sortDir' parameters determine ordering of results
func (s *Server) FindAssetsWithDataForTask(p Params) (assets []Asset, m meta, err error) {
	var exists []string

	taskParams := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, m, err := s.FindTasks(taskParams)
	if p.Task != "" {
		exists = append(exists, fmt.Sprintf(`{ "exists": { "field": "SubmittedData.%s" } }`, p.Task))
	} else {
		if err != nil {
			return
		}
		for _, t := range tasks {
			exists = append(exists, fmt.Sprintf(`{ "exists": { "field": "SubmittedData.%s" } }`, t.Name))
		}
	}
	searchQuery := `{
		"query": {
			"filtered": {
				"filter":   {
					"bool": {
						"must": [%s]
					}
				}
			}
		},
		"from": %s,
		"size": %s,
		"sort": [ { "%s": { "order" : "%s" } } ]
	}`

	searchJson := fmt.Sprintf(searchQuery, strings.Join(exists, ", "), p.From, p.Size, p.SortBy, p.SortDir)
	log.Println(searchJson)
	results, err := s.EsConn.Search(s.Index, "assets", nil, searchJson)
	if err != nil {
		return
	}

	m.Total = results.Hits.Total
	m.From, _ = strconv.Atoi(p.From)
	m.Size, _ = strconv.Atoi(p.Size)

	for _, hit := range results.Hits.Hits {
		var asset Asset
		rawMessage := hit.Source
		err = json.Unmarshal(*rawMessage, &asset)
		if err != nil {
			return
		}
		assets = append(assets, asset)
	}
	return
}

// FindAssignmentAsset returns an eligible asset for a given task and user, basing this on AssignmentCriteria.
// It is called from CreateAssignment.
func (s *Server) FindAssignmentAsset(task Task, user User) (Asset, error) {
	var assignmentAsset Asset
	var assetIds []string

	assetQuery := fmt.Sprintf(`{
  "query": {
    "bool": {
      "must": [
        {
          "term": {
            "assignments.Task": "%s"
          }
				},
        {
          "term": {
            "assignments.User": "%s"
          }
				},
				{
					"term": {
						"assignments.Project": "%s"
					}
				}
				]
			}
		},
		"from": 0,
		"size": %d
	}`, task.Id, user.Id, s.ActiveProjectId, user.Counts["Assignments"])
	assetResults, err := s.EsConn.Search(s.Index, "assignments", nil, assetQuery)
	if err != nil {
		return assignmentAsset, err
	}
	for _, hit := range assetResults.Hits.Hits {
		idParts := strings.Split(hit.Id, "HIVE")
		assetIds = append(assetIds, idParts[2])
	}

	// the parts of a 'bool' query - so far no need for 'should'
	musts := []string{}
	mustNots := []string{}

	// build up the pieces of the full elasticsearch query
	for taskName, ruleI := range task.AssignmentCriteria.SubmittedData {
		rule := ruleI.(map[string]interface{})

		// an empty rule means assets should have no data submitted for this task
		if len(rule) == 0 {
			tmpl := `{
				"missing": {
					"field": "SubmittedData.%s"
				}
			}`

			musts = append(musts, fmt.Sprintf(tmpl, task.Name))

			// assets must have data submitted that exactly matches the rule
		} else {
			for fieldName, fieldValue := range rule {
				tmpl := `{
					"query": {
						"match": {
							"SubmittedData.%s.%s": "%s"
						}
					}
				}`
				musts = append(musts, fmt.Sprintf(tmpl, taskName, fieldName, fieldValue))
			}
		}
	}

	// limit query results to assets in this project
	projectTmpl := `{
		"query": {
			"match": {
				"Project": "%s"
			}
		}
	}`
	musts = append(musts, fmt.Sprintf(projectTmpl, s.ActiveProjectId))

	if len(assetIds) > 0 {
		assetTmpl := `{ "query": { "terms": { "Id": [ %s ] } } }`
		assetIdString := "\"" + strings.Join(assetIds, "\",\"") + "\""
		mustNots = append(mustNots, fmt.Sprintf(assetTmpl, assetIdString))
	}

	mustsJson := strings.Join(musts, ", ")
	mustNotsJson := strings.Join(mustNots, ", ")

	var args map[string]interface{}
	matchAllQuery := `{ "query": { "match_all" : { } } }`
	countResponse, err := s.EsConn.Count(s.Index, "assets", args, matchAllQuery)
	if err != nil {
		return assignmentAsset, err
	}

	// finally, compose the entire filtered query
	searchQuery := fmt.Sprintf(
		`{"query":{"filtered":{"filter":{"bool":{"must":[%s],"must_not":[%s]}}}},"from":0,"size":%d}`, mustsJson, mustNotsJson, countResponse.Count)

	results, err := s.EsConn.Search(s.Index, "assets", nil, searchQuery)
	if err != nil {
		return assignmentAsset, err
	}

	if results.Hits.Total <= 0 {
		err = errors.New("No assets found")
		return assignmentAsset, err

	} else {
		randomHit := rand.Intn(len(results.Hits.Hits))
		rawMessage := results.Hits.Hits[randomHit].Source
		err = json.Unmarshal(*rawMessage, &assignmentAsset)
		if err != nil {
			return assignmentAsset, err
		}
	}
	return assignmentAsset, nil
}

// FindAssignment looks up an assignment by id.
func (s *Server) FindAssignment(id string) (assignment *Assignment, err error) {

	err = s.EsConn.GetSource(s.Index, "assignments", id, nil, &assignment)
	if err != nil {
		return nil, err
	}
	return assignment, nil
}

func (s *Server) RootHandler(w http.ResponseWriter, r *http.Request) {
	endpointsJson := `{"status": "ok"}`
	s.wrapResponse(w, r, 200, []byte(endpointsJson))
}

// Creates or updates a project in Hive
//		POST /admin/projects

// @Title AdminProjectsHandler
// @Description returns a paginated list of projects in Hive
// @Accept  json
// @Param   from        query   int     false        "If specified, will return a set of projects starting with from number"
// @Param   size        query   int     false        "If specified, will return a total number of projects specified as size"
// @Success 200 {object}  projectsResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /projects
// @Router /admin/projects [get]
func (s *Server) AdminProjectsHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	p := Params{
		From:    defaultQuery(queryParams, "from", "0"),
		Size:    defaultQuery(queryParams, "size", "10"),
		SortBy:  defaultQuery(queryParams, "sortBy", "Id"),
		SortDir: defaultQuery(queryParams, "sortDir", "asc"),
	}

	projects, m, err := s.FindProjects(p)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	resp := projectsResponse{
		Projects: projects,
		Meta:     m,
	}
	projectsJson, err := json.Marshal(resp)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, projectsJson)
}

// @Title AdminProjectHandler
// @Description returns a project by ID
// @Accept  json
// @Param   project_id        path   string     true        "Project ID"
// @Success 200 {object}  projectResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /projects
// @Router /admin/projects/{project_id} [get]
func (s *Server) AdminProjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	var project *Project
	var err error

	project, err = s.FindProject(s.ActiveProjectId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	// format the json response
	resp := projectResponse{
		Project: *project,
	}
	projectJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, projectJson)
}

// @Title AdminCreateProjectHandler
// @Description creates or updates a project
// @Accept  json
// @Param   project_id        path   string     true        "Project ID"
// @Success 200 {object}  projectResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /projects
// @Router /admin/projects/{project_id} [post]
func (s *Server) AdminCreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	var project *Project
	var err error

	project, err = s.CreateProject(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	resp := projectResponse{
		Project: *project,
	}
	projectJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, projectJson)
}

// @Title ProjectHandler
// @Description returns a project by ID
// @Accept  json
// @Param   project_id        path   string     true        "Project ID"
// @Success 200 {object}  projectResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /projects
// @Router /projects/{project_id} [get]
func (s *Server) ProjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	var project *Project
	var err error

	project, err = s.FindProject(vars["project_id"])
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	// format the json response
	resp := projectResponse{
		Project: *project,
	}
	projectJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, projectJson)
}

// @Title AssetHandler
// @Description returns public info for a single asset by ID
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   asset_id        path   string     true        "Asset ID"
// @Success 200 {object} assetResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /projects/{project_id}/assets/{asset_id} [get]
func (s *Server) AssetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	assetId := vars["asset_id"]
	s.ActiveProjectId = vars["project_id"]

	asset, err := s.FindAsset(assetId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	resp := assetResponse{
		Asset: *asset,
	}
	assetJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assetJson)
}

// @Title AdminTaskHandler
// @Description returns info for a single task by ID
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id        path   string     true        "Task ID"
// @Success 200 {object} taskResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /admin/projects/{project_id}/tasks/{task_id} [get]
func (s *Server) AdminTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	taskId := vars["task_id"]
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskId = s.ActiveProjectId + "-" + vars["task_id"]
	}

	task, err := s.FindTask(taskId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	taskJson, err := json.Marshal(taskResponse{
		Task: *task,
	})
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, taskJson)
}

// @Title AdminCreateTaskHandler
// @Description creates or updates a task in a project
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id        path   string     true        "Task ID"
// @Success 200 {object} taskResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /projects/{project_id}/tasks/{task_id} [get]
func (s *Server) AdminCreateTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	task, err := s.CreateTask(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	taskJson, err := json.Marshal(taskResponse{
		Task: *task,
	})
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, taskJson)
}

// @Title TaskHandler
// @Description returns public info for a single task by ID
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id        path   string     true        "Task ID"
// @Success 200 {object} taskResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /tasks
// @Router /projects/{project_id}/tasks/{task_id} [get]
func (s *Server) TaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	taskId := vars["task_id"]
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskId = s.ActiveProjectId + "-" + vars["task_id"]
	}

	task, err := s.FindTask(taskId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	taskJson, err := json.Marshal(taskResponse{
		Task: *task,
	})
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, taskJson)
}

// @Title AssignmentHandler
// @Description returns public info for a single assignment by ID
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   assignment_id        path   string     true        "Assignment ID"
// @Success 200 {object} assignmentResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assignments
// @Router /projects/{project_id}/assignments/{assignment_id} [get]
func (s *Server) AssignmentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	assignmentId := vars["assignment_id"]

	assignment, err := s.FindAssignment(assignmentId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// format the json response
	resp := assignmentResponse{
		Assignment: *assignment,
	}
	assignmentJson, err := json.Marshal(resp)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assignmentJson)
}

// Looks for a cookie named 'cookieName' in the request.
// If the cookie is found, returns its value.
// Otherwise returns an empty string.
func (s *Server) FindCookieValue(r *http.Request, cookieName string) (cookieValue string) {
	cookie, err := r.Cookie(cookieName)

	// failed to find the cookie
	if err != nil {
		return ""
	}

	// cookie is empty
	if len(cookie.Value) == 0 || cookie.Value == "" {
		return ""
	}
	// found the cookie, return its value
	return cookie.Value
}

// Creates a user based on the JSON body of the request.
func (s *Server) CreateUser(requestBody io.Reader) (user *User, err error) {

	body, err := ioutil.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &user)
	if err != nil {
		return nil, err
	}

	user.Project = s.ActiveProjectId
	user.Favorites = userFavorites{}

	user.Counts = Counts{
		"Favorites":      0,
		"Assignments":    0,
		"VerifiedAssets": 0,
	}

	taskParams := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, _, err := s.FindTasks(taskParams)
	if err != nil {
		for _, task := range tasks {
			user.Counts[task.Id] = 0
		}
	}

	// store user in elasticsearch
	// if user.Id is blank, es will generate a new one
	// if user.Id is NOT blank, es will store the user with that id
	result, err := s.EsConn.Index(s.Index, "users", user.Id, nil, user)
	if err != nil {
		return user, err
	}

	// if the user didn't have an autogenerated id, store it now
	if len(user.Id) == 0 {
		user.Id = result.Id
		_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
		if err != nil {
			return user, err
		}
	}

	return user, nil
}

// Creates a user account with a given user id, called when a user has a {project_id}_user_id but no matching record is found.
// in other words, this method is used in edge cases.
func (s *Server) CreateUserFromMissingCookieValue(userId string) (User, error) {
	var err error

	user := User{
		Id:      userId,
		Project: s.ActiveProjectId,
	}
	user.Favorites = userFavorites{}
	user.Counts = Counts{
		"Favorites":      0,
		"Assignments":    0,
		"VerifiedAssets": 0,
	}

	taskParams := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, _, err := s.FindTasks(taskParams)
	if err != nil {
		for _, task := range tasks {
			user.Counts[task.Id] = 0
		}
	}

	// store user in elasticsearch
	// if user.Id is blank, es will generate a new one
	// if user.Id is NOT blank, es will store the user with that id
	result, err := s.EsConn.Index(s.Index, "users", user.Id, nil, user)
	if err != nil {
		return user, err
	}

	// if the user didn't have an autogenerated id, store it now
	if len(user.Id) == 0 {
		user.Id = result.Id
		_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
		if err != nil {
			return user, err
		}
	}

	return user, nil
}

// Creates a user account with a given ExternalId. This method is used to link user accounts from third
// party/external registration systems into hive.
func (s *Server) CreateExternalUser(externalId string) (User, error) {
	var user User
	user.ExternalId = externalId
	user.Project = s.ActiveProjectId
	user.Favorites = userFavorites{}
	user.Counts = Counts{
		"Favorites":      0,
		"Assignments":    0,
		"VerifiedAssets": 0,
	}

	taskParams := Params{
		From:    "0",
		Size:    "10",
		SortBy:  "Name",
		SortDir: "asc",
	}

	tasks, _, err := s.FindTasks(taskParams)
	if err != nil {
		for _, task := range tasks {
			user.Counts[task.Id] = 0
		}
	}

	// store user in elasticsearch
	// if user.Id is blank, es will generate a new one
	// if user.Id is NOT blank, es will store the user with that id
	result, err := s.EsConn.Index(s.Index, "users", user.Id, nil, user)
	if err != nil {
		return user, err
	}

	// if the user didn't have an autogenerated id, store it now
	if len(user.Id) == 0 {
		user.Id = result.Id
		_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
		if err != nil {
			return user, err
		}
	}
	return user, nil
}

// @Title FavoriteHandler
// @Description toggles favoriting on an asset for the current user
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   asset_id        path   string     true        "Retrieve asset with given ID only"
// @Param   user_id        header   string     true        "User ID stored in a cookie named according to the project '{project_id}_user_id'"
// @Success 200 {object} favoriteResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /projects/{project_id}/assets/{asset_id}/favorite [get]
func (s *Server) FavoriteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	// find the asset
	asset, err := s.FindAsset(vars["asset_id"])
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// find the user
	sessionCookieName := s.ActiveProjectId + "_user_id"
	userId := s.FindCookieValue(r, sessionCookieName)
	user, err := s.FindUser(userId)
	if user == nil {
		s.wrapResponse(w, r, 500, s.wrapError(errors.New("Favoriting assets requires a valid user.")))
		return
	}

	faveResponse := favoriteResponse{AssetId: asset.Id, Action: "favorited"}

	if len(asset.Counts) <= 0 {
		asset.Counts = Counts{
			"Favorites":   0,
			"Assignments": 0,
			"finished":    0,
			"skipped":     0,
			"unfinished":  0,
		}
	}
	if len(user.Favorites) <= 0 {
		user.Favorites = userFavorites{}
	}
	// is this asset in the user's favorites?
	_, ok := user.Favorites[asset.Id]
	if ok {
		delete(user.Favorites, asset.Id)
		faveResponse.Action = "unfavorited"
		if asset.Counts["Favorites"] > 0 {
			asset.Counts["Favorites"] -= 1
		}
	} else {
		// add the asset to the user's favorites
		user.Favorites[asset.Id] = *asset
		asset.Counts["Favorites"] += 1
	}
	user.Counts["Favorites"] = len(user.Favorites)

	_, err = s.EsConn.Index(s.Index, "assets", asset.Id, nil, asset)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	responseJson, err := json.Marshal(faveResponse)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	s.wrapResponse(w, r, 200, responseJson)
}

// @Title FavoritesHandler
// @Description returns a paginated list of favorited assets for the current user
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   user_id        header   string     true        "User ID stored in a cookie named according to the project '{project_id}_user_id'"
// @Param   size        query   int     false        "If specified, will return a total number of assets specified as size"
// @Param   size        query   int     false        "If specified, will return a total number of assets specified as size"
// @Success 200 {object} favoritesResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /projects/{project_id}/user/favorites [get]
func (s *Server) FavoritesHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	sessionCookieName := s.ActiveProjectId + "_user_id"
	userId := s.FindCookieValue(r, sessionCookieName)
	user, err := s.FindUser(userId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	queryParams := r.URL.Query()
	p := Params{
		From: defaultQuery(queryParams, "from", "0"),
		Size: defaultQuery(queryParams, "size", "10"),
	}

	from, _ := strconv.Atoi(p.From)
	size, _ := strconv.Atoi(p.Size)

	m := meta{
		Total: len(user.Favorites),
		From:  from,
		Size:  size,
	}

	resp := favoritesResponse{
		Favorites: user.Favorites,
		Meta:      m,
	}
	favoritesJson, err := json.Marshal(resp)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, favoritesJson)
}

// @Title CompleteTaskHandler
// @Description updates assets matching task CompletionCriteria with SubmittedData
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id     path    string     true        "Task ID"
// @Success 200 {object}  assetsResponse
// @Failure 500 {object} error	appropriate error message
// @Resource /assets
// @Router /admin/projects/{project_id}/tasks/{task_id}/complete [get]
func (s *Server) CompleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]

	assets, err := s.CompleteTask(taskId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	assetsJson, err := json.Marshal(assetsResponse{
		Assets: assets,
	})
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	s.wrapResponse(w, r, 200, assetsJson)
}

// @Title UserHandler
// @Description returns info for the current user, creating a matching record if none found
// @Description creates a user in a project
// @Param   project_id     path    string     true        "Project ID"
// @Param   user_id        header   string     true        "User ID stored in a cookie named according to the project '{project_id}_user_id'"
// @Success 200 {object}  User
// @Failure 500 {object} error	appropriate error message
// @Resource /users
// @Router /projects/{project_id}/user [get]
func (s *Server) UserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	// user id is stored in a cookie named according to the project
	sessionCookieName := s.ActiveProjectId + "_user_id"

	// look for project's user session cookie
	userId := s.FindCookieValue(r, sessionCookieName)

	// try to find a matching user
	user, err := s.FindUser(userId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	// FindUser returns nil if no matching user is found
	if user == nil {
		tmpUser, err := s.CreateUserFromMissingCookieValue(userId)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}
		user = &tmpUser
	}

	userJson, err := json.Marshal(user)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, userJson)
}

// @Title CreateUserHandler
// @Description creates a user in a project
// @Param   project_id     path    string     true        "Project ID"
// @Param   userdata        body   string     true        "JSON-formatted user data"
// @Success 200 {object}  User
// @Failure 500 {object} error	appropriate error message
// @Resource /users
// @Router /projects/{project_id}/user [post]
func (s *Server) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]

	user, err := s.CreateUser(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
	}

	userJson, err := json.Marshal(user)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, userJson)
}

// @Title ExternalUserHandler
// @Description finds or creates a user by external ID
// @Param   userdata        body   string     true        "JSON-formatted user data including ExternalId (3rd party uid) and Id (hive uid)"
// @Param	connect	path	boolean	false "If specified, will try merging matching user records by ExternalId and/or Id"
// @Success 200 {object}  User
// @Failure 500 {object} error	appropriate error message
// @Resource /users
// @Router /projects/{project_id}/user/external/{connect} [post]
func (s *Server) ExternalUserHandler(w http.ResponseWriter, r *http.Request) {
	var user *User
	var externalUser User
	var err error

	vars := mux.Vars(r) // params in URL
	connectAccounts := vars["connect"]
	s.ActiveProjectId = vars["project_id"]

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	var lookupData struct {
		Id         string
		ExternalId string
	}

	err = json.Unmarshal(body, &lookupData)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	query := elastigo.Search(s.Index).Type("users").Filter(
		elastigo.Filter().Terms("ExternalId", lookupData.ExternalId),
		elastigo.Filter().Terms("Project", s.ActiveProjectId),
	)
	results, err := query.Result(&s.EsConn)

	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	resultCount := results.Hits.Total
	if resultCount != 0 {
		err = json.Unmarshal(*results.Hits.Hits[0].Source, &externalUser)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}

		if externalUser.ExternalId == "0" {
			resultCount = 0
		}
	}

	// found no matching users
	if resultCount == 0 {
		userId := lookupData.Id

		if userId == "" && lookupData.Id != "" {
			userId = lookupData.Id
		}

		if userId == "" {
			// no ${project_id}_user_id set, create a new user
			tmpUser, err := s.CreateExternalUser(lookupData.ExternalId)
			if err != nil {
				s.wrapResponse(w, r, 500, s.wrapError(err))
				return
			}
			user = &tmpUser

		} else {
			// ${project_id}_user_id set, try looking up the user
			tmpUser, err := s.FindUser(userId)
			if err != nil {
				s.wrapResponse(w, r, 500, s.wrapError(err))
				return
			}

			user = tmpUser
			// found a user, set the externalId on it
			if user != nil {
				user.ExternalId = lookupData.ExternalId
				_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
				if err != nil {
					s.wrapResponse(w, r, 500, s.wrapError(err))
					return
				}

			} else {
				// failed finding a user for that cookie (how would we get here?)
				*user, err = s.CreateExternalUser(lookupData.ExternalId)
				if err != nil {
					s.wrapResponse(w, r, 500, s.wrapError(err))
					return
				}
			}
		}
	}

	// found a matching user
	if resultCount == 1 {
		err = json.Unmarshal(*results.Hits.Hits[0].Source, &externalUser)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}

		if connectAccounts == "" {
			user = &externalUser
		} else {
			userId := lookupData.Id
			tmpUser, err := s.FindUser(userId)
			if err != nil {
				s.wrapResponse(w, r, 500, s.wrapError(err))
				return
			}
			user = tmpUser
			if user != nil {
				user.ExternalId = lookupData.ExternalId

				// merge all the things

				// first: contribution counts
				for key, count := range externalUser.Counts {
					user.Counts[key] += count
				}

				// second: favorites
				for key, value := range externalUser.Favorites {
					user.Favorites[key] = value
				}

				user.Counts["VerifiedAssets"] = len(user.VerifiedAssets)

				_, err = s.EsConn.Index(s.Index, "users", user.Id, nil, user)
				if err != nil {
					s.wrapResponse(w, r, 500, s.wrapError(err))
					return
				}

				// now, kill the other account
				var args map[string]interface{}
				_, err := s.EsConn.Delete(s.Index, "users", externalUser.Id, args)
				if err != nil {
					s.wrapResponse(w, r, 500, s.wrapError(err))
					return
				}
			}
		}
	}

	if resultCount > 1 {
		s.wrapResponse(w, r, 500, s.wrapError(errors.New("found more than one user with this externalId")))
		return
	}

	userJson, err := json.Marshal(user)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, userJson)
	return
}

// @Title AssignAssetHandler
// @Description finds or creates an unfinished assignment for the given _asset_, task and current user.
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id     path    string     true        "Task ID"
// @Param   asset_id        path   string     true        "Asset ID"
// @Success 200 {object} Assignment
// @Failure 500 {object} error	appropriate error message
// @Resource /assignments
// @Router /projects/{project_id}/tasks/{task_id}/assets/{asset_id}/assignments [get]
//
// r.HandleFunc("/projects/{project_id}/tasks/{task_id}/assets/{asset_id}/assignments", s.AssignAssetHandler).Methods("GET")
func (s *Server) AssignAssetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]
	assetId := vars["asset_id"]

	// make sure taskId includes the active project
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskId = s.ActiveProjectId + "-" + vars["task_id"]
	}

	// get user id from session cookie
	userId := s.FindCookieValue(r, s.ActiveProjectId+"_user_id")
	if userId == "" {
		userError := errors.New("Assignments can't be created without a user.")
		s.wrapResponse(w, r, 500, s.wrapError(userError))
		return
	}

	assignment, err := s.CreateAssetAssignment(taskId, userId, assetId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	assignJson, err := json.Marshal(assignment)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assignJson)
}

// @Title UserCreateAssignmentHandler
// @Description finishes a task assignment & assigns a new one for the current user.
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id     path    string     true        "Task ID"
// @Param   assignment        body   string     true        "JSON-formatted assignment including user submitted data"
// @Param   user_id        header   string     true        "User ID stored in a cookie named according to the project '{project_id}_user_id'"
// @Success 200 {object}  Assignment
// @Failure 500 {object} error	appropriate error message
// @Resource /assignments
// @Router /projects/{project_id}/tasks/{task_id}/assignments [post]
func (s *Server) UserCreateAssignmentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskId = s.ActiveProjectId + "-" + vars["task_id"]
	}

	// get user id from session cookie
	userId := s.FindCookieValue(r, s.ActiveProjectId+"_user_id")

	_, err := s.UpdateAssignment(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	assignment, err := s.CreateAssignment(taskId, userId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	assignJson, err := json.Marshal(assignment)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assignJson)
	return
}

// @Title UserAssignmentHandler
// @Description finds or creates an unfinished task assignment for the current user.
// @Accept  json
// @Param   project_id     path    string     true        "Project ID"
// @Param   task_id     path    string     true        "Task ID"
// @Param   user_id        header   string     true        "User ID stored in a cookie named according to the project '{project_id}_user_id'"
// @Success 200 {object}  Assignment
// @Failure 500 {object} error	appropriate error message
// @Resource /assignments
// @Router /projects/{project_id}/tasks/{task_id}/assignments [get]
func (s *Server) UserAssignmentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // params in URL
	s.ActiveProjectId = vars["project_id"]
	taskId := vars["task_id"]
	if !strings.HasPrefix(vars["task_id"], s.ActiveProjectId) && vars["task_id"] != "" {
		taskId = s.ActiveProjectId + "-" + vars["task_id"]
	}

	// get user id from session cookie
	sessionCookie, err := r.Cookie(s.ActiveProjectId + "_user_id")
	if err != nil { // TODO: figure out how to avoid getting here; frontend should check for user cookie before calling assign
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	userId := sessionCookie.Value

	assignment, err := s.CreateAssignment(taskId, userId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	assignJson, err := json.Marshal(assignment)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	s.wrapResponse(w, r, 200, assignJson)
}

// Admin endpoint clears out db, configures elasticsearch and creates a project
//		ANY /admin/setup
// WARNING: this empties your database. Really.
// @Title AdminSetupHandler
// @Description finds or creates an unfinished task assignment for the current user.
// @Accept  json
// @Param   DELETE_MY_DATABASE     path    string     false        "If specified exactly with the value 'YES_I_AM_SURE', this will wipe out the entire Hive index, removing all data. Please be sure you want to do this!"
// @Success 200 {object}  Assignment
// @Failure 500 {object} error	appropriate error message
// @Resource /projects
// @Router /admin/setup/{DELETE_MY_DATABASE} [get]
func (s *Server) AdminSetupHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	log.Println("Importing data into hive...")

	log.Println("Step 1: configuring elasticsearch.")
	indexExists, possible404 := s.EsConn.IndicesExists(s.Index)

	// for reasons mysterious to me, elastigo wraps all of the http pkg's functions
	// and does not check if the response to IndicesExists is a 404.
	// Elasticsearch will respond with a 404 if the index does not exist.
	// Here we check for this and correctly set the value of indexExists to false
	if possible404 != nil && possible404.Error() == "record not found" {
		indexExists = false

		// otherwise some other error was thrown, so just 500 and give up here.
	} else if possible404 != nil {
		s.wrapResponse(w, r, 500, s.wrapError(possible404))
		return
	}

	if vars["DELETE_MY_DATABASE"] == "YES_I_AM_SURE" && indexExists {
		// Delete existing hive index (was: curl -XDELETE localhost:9200/hive  >/dev/null 2>&1)
		_, err := s.EsConn.DeleteIndex(s.Index)
		if err != nil {
			log.Println("Failed to delete index:", err)
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}
		log.Println("Deleted index", s.Index, ". I hope that was ok - you said you were sure!")
		indexExists = false
	} else if indexExists {
		giveUpErr := fmt.Errorf("Index '%s' exists. Use a different value or add 'YES_I_AM_SURE' to delete it: /admin/setup/YES_I_AM_SURE.", s.Index)
		s.wrapResponse(w, r, 500, s.wrapError(giveUpErr))
		return
	}

	if !indexExists {
		log.Println("Creating index", s.Index)
		// Create hive index (was: curl -XPOST localhost:9200/hive >/dev/null 2>&1)
		_, err := s.EsConn.CreateIndex(s.Index)
		if err != nil {
			s.wrapResponse(w, r, 500, s.wrapError(err))
			return
		}
	}

	assignmentsBody := `{
		"assignments": {
			"properties": {
				"Asset": {
					"properties": {
						"Favorited": {
							"type": "boolean"
						},
						"Id": {
							"type": "string",
							"index": "not_analyzed"
						},
						"Url": {
							"type": "string",
							"index": "not_analyzed"
						}
					}
				},
				"Id": {
					"type": "string",
					"index": "not_analyzed"
				},
				"Project": {
					"type": "string",
					"index": "not_analyzed"
				},
				"State": {
					"type": "string",
					"index": "not_analyzed"
				},
				"Task": {
					"type": "string",
					"index": "not_analyzed"
				},
				"User": {
					"type": "string",
					"index": "not_analyzed"
				}
			}
		}
	}`

	_, err := s.EsConn.DoCommand("PUT", fmt.Sprintf("/%s/%s/_mapping", s.Index, "assignments"), nil, assignmentsBody)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	log.Println("Done configuring elasticsearch")

	log.Println("Step 2: creating project.")

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	var importedJson struct {
		Project Project
		Tasks   []Task
		Assets  []Asset
	}

	err = json.Unmarshal(body, &importedJson)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}

	s.ActiveProjectId = importedJson.Project.Id

	// store in elasticsearch
	_, err = s.EsConn.Index(s.Index, "projects", s.ActiveProjectId, nil, importedJson.Project)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	log.Println("Done creating project:", s.ActiveProjectId)

	log.Println("Step 3: importing tasks.")

	tasks, _, err := s.importTasks(importedJson.Tasks)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	log.Println("Done creating tasks:", len(tasks))

	log.Println("Step 4: adding assets.")

	assetsBody := `{
		"assets": {
			"properties": {
				"Id": {
					"type": "string",
					"index": "not_analyzed"
				},
				"Metadata": {
					"properties": {
						%s
					}
				},
				"Project": {
					"type": "string"
				},
				"SubmittedData": {
					"type": "nested",
					"include_in_parent": true,

					"properties": {
						%s
					}
				},
				"Url": {
					"type": "string"
				}
			}
		}
	}`

	project, err := s.FindProject(s.ActiveProjectId)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	var metaProperties []string
	for _, metaProp := range project.MetaProperties {
		metaProperties = append(metaProperties, fmt.Sprintf(`"%s": { "type": "%s", "index": "not_analyzed" }`, metaProp.Name, metaProp.Type))
	}
	metaPropertiesString := strings.Join(metaProperties, ",")

	var taskProperties []string
	for _, task := range tasks {
		taskProperties = append(taskProperties, fmt.Sprintf(`"%s": { "type": "object" }`, task.Name))
	}
	taskPropertiesString := strings.Join(taskProperties, ",")
	assetsMapping := fmt.Sprintf(assetsBody, metaPropertiesString, taskPropertiesString)

	_, err = s.EsConn.DoCommand("PUT", fmt.Sprintf("/%s/%s/_mapping", s.Index, "assets"), nil, assetsMapping)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	assets, err := s.importAssets(importedJson.Assets)
	if err != nil {
		s.wrapResponse(w, r, 500, s.wrapError(err))
		return
	}
	log.Println("Done adding", len(assets), "assets")

	report := []byte(fmt.Sprintf(`{"status":"200 OK", "Project": "%s", "Tasks": "%d", "Assets": "%d"}`, s.ActiveProjectId, len(tasks), len(assets)))
	s.wrapResponse(w, r, 200, report)
	return
}

// Starts up hive-server on the specified port, connecting to Elasticsearch at {esDomain}:{esPort} using the given index.
// Default parameters:
//		hive port: 8080
//		elasticsearch domain: localhost
//		elasticsearch port: 9200
//		elasticsearch index: hive
func (s *Server) Run() {
	log.Println("running hive-server on port", s.Port, "storing data in elasticsearch under index", s.Index)

	r := mux.NewRouter()
	r.StrictSlash(true)

	// ANY / - lists endpoints
	r.HandleFunc("/", s.RootHandler)

	// ANY /admin/setup - clears out db, configures elasticsearch and creates a project
	r.HandleFunc("/admin/setup", s.AdminSetupHandler)
	r.HandleFunc("/admin/setup/{DELETE_MY_DATABASE}", s.AdminSetupHandler)

	// GET /admin/projects - returns all projects in Hive
	r.HandleFunc("/admin/projects", s.AdminProjectsHandler).Methods("GET")

	// GET /admin/projects/{project_id} - returns project information
	r.HandleFunc("/admin/projects/{project_id}", s.AdminProjectHandler).Methods("GET")

	// POST /admin/projects/{project_id} - creates or updates a project
	r.HandleFunc("/admin/projects/{project_id}", s.AdminCreateProjectHandler).Methods("POST")

	// GET /admin/projects/{project_id}/tasks - returns tasks in this project
	r.HandleFunc("/admin/projects/{project_id}/tasks", s.AdminTasksHandler).Methods("GET")

	// POST /admin/projects/{project_id}/tasks - imports tasks into this project
	r.HandleFunc("/admin/projects/{project_id}/tasks", s.AdminCreateTasksHandler).Methods("POST")

	// GET /admin/projects/{project_id}/tasks/{task_id} - returns task information
	r.HandleFunc("/admin/projects/{project_id}/tasks/{task_id}", s.AdminTaskHandler).Methods("GET")

	// POST /admin/projects/{project_id}/tasks/{task_id} - create or update a task
	r.HandleFunc("/admin/projects/{project_id}/tasks/{task_id}", s.AdminCreateTaskHandler).Methods("POST")

	// enable and disable tasks
	r.HandleFunc("/admin/projects/{project_id}/tasks/{task_id}/enable", s.EnableTaskHandler).Methods("GET")
	r.HandleFunc("/admin/projects/{project_id}/tasks/{task_id}/disable", s.DisableTaskHandler).Methods("GET")

	// GET /admin/projects/{project_id}/assets - returns assets in this project
	// GET /admin/projects/{project_id}/assets?from=10&size=30 - paginates assets
	// GET /admin/projects/{project_id}/assets?task=:task&state=:state - returns a list of assets based on task and state
	r.HandleFunc("/admin/projects/{project_id}/assets", s.AdminAssetsHandler).Methods("GET")

	// POST /admin/projects/{project_id}/assets - imports assets into this project
	r.HandleFunc("/admin/projects/{project_id}/assets", s.AdminCreateAssetsHandler).Methods("POST")

	// GET /admin/projects/{project_id}/assets/{asset_id} - get a single asset's data
	r.HandleFunc("/admin/projects/{project_id}/assets/{asset_id}", s.AdminAssetHandler)

	// GET /admin/projects/{project_id}/tasks/{task_id}/complete - mark any assets completed for this task
	r.HandleFunc("/admin/projects/{project_id}/tasks/{task_id}/complete", s.CompleteTaskHandler)

	// GET /admin/projects/{project_id}/users - returns users in this project
	// GET /admin/projects/{project_id}/users?from=0&size=10 - paginates users
	r.HandleFunc("/admin/projects/{project_id}/users", s.AdminUsersHandler)

	// GET /admin/projects/{project_id}/users/{user_id} - returns a single user in this project
	r.HandleFunc("/admin/projects/{project_id}/users/{user_id}", s.AdminUserHandler)

	// GET /admin/projects/{project_id}/assignments?task={task_id}&state={state}
	// GET /admin/projects/{project_id}/assignments?task={task_id}&state={state}&from=from&size=size
	r.HandleFunc("/admin/projects/{project_id}/assignments", s.AdminAssignmentsHandler)

	// GET /projects/{project_id}/tasks/{task_id} - returns task information
	r.HandleFunc("/projects/{project_id}/tasks/{task_id}", s.TaskHandler).Methods("GET")

	// GET /projects/{project_id}/tasks/find/assignments - returns a new assignment for the given task + current user
	r.HandleFunc("/projects/{project_id}/tasks/{task_id}/assignments", s.UserAssignmentHandler).Methods("GET")

	// POST /projects/{project_id}/tasks/find/assignments - submit assignment (contribute, fill in form, etc)
	r.HandleFunc("/projects/{project_id}/tasks/{task_id}/assignments", s.UserCreateAssignmentHandler).Methods("POST")

	// GET /projects/{project_id} - returns project information
	r.HandleFunc("/projects/{project_id}", s.ProjectHandler).Methods("GET")

	// GET /projects/{project_id}/assets/SOPB9LrQTRyKeQCi4xDdTA - returns asset information
	r.HandleFunc("/projects/{project_id}/assets/{asset_id}", s.AssetHandler).Methods("GET")

	// GET /projects/{project_id}/tasks - returns tasks in this project
	r.HandleFunc("/projects/{project_id}/tasks", s.TasksHandler).Methods("GET")

	// GET /projects/{project_id}/tasks/find/assets/W1fpeD0lQs2tR1R4OqkzAQ/assignments - returns a new assignment for task + asset + current user
	r.HandleFunc("/projects/{project_id}/tasks/{task_id}/assets/{asset_id}/assignments", s.AssignAssetHandler).Methods("GET")

	// GET /projects/{project_id}/user - returns user information based on project session cookie
	r.HandleFunc("/projects/{project_id}/user", s.UserHandler).Methods("GET")

	// POST /projects/{project_id}/user - creates a user based on json data posted
	r.HandleFunc("/projects/{project_id}/user", s.CreateUserHandler).Methods("POST")

	// POST /projects/{project_id}/user/external - looks up user by external id, returns session token
	r.HandleFunc("/projects/{project_id}/user/external", s.ExternalUserHandler).Methods("POST")
	r.HandleFunc("/projects/{project_id}/user/external/{connect}", s.ExternalUserHandler).Methods("POST")

	// GET /projects/{project_id}/assets/SOPB9LrQTRyKeQCi4xDdTA/favorite - favorites an asset
	r.HandleFunc("/projects/{project_id}/assets/{asset_id}/favorite", s.FavoriteHandler).Methods("GET")

	// GET /projects/{project_id}/user/favorites - returns a user's favorited ads
	r.HandleFunc("/projects/{project_id}/user/favorites", s.FavoritesHandler).Methods("GET")

	// GET /projects/{project_id}/assignments/{assignment} - returns assignment information
	r.HandleFunc("/projects/{project_id}/assignments/{assignment_id}", s.AssignmentHandler).Methods("GET")

	http.Handle("/", r)
	err := http.ListenAndServe(":"+s.Port, nil)
	if err != nil {
		log.Fatalf(err.Error())
	}
}
