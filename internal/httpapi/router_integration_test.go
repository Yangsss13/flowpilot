package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"minikvx-agent/internal/config"
	"minikvx-agent/internal/database"
	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/handler"
	"minikvx-agent/internal/httpapi"
	"minikvx-agent/internal/repository"
	"minikvx-agent/internal/service"
)

func TestTaskQueryEndpointsWithMySQL(t *testing.T) {
	if os.Getenv("MINIKVX_INTEGRATION") != "1" {
		t.Skip("set MINIKVX_INTEGRATION=1 to run MySQL integration tests")
	}

	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}

	taskRepository := repository.NewGormTaskRepository(db)
	taskService := service.NewTaskService(taskRepository)
	router := httpapi.NewRouter(handler.NewTaskHandler(taskService))

	name := "query-integration-" + time.Now().Format("20060102150405.000000000")
	createBody := []byte(`{
		"name":"` + name + `",
		"steps":[
			{"name":"first","action_type":"sleep","action_payload":{"duration_ms":1}},
			{"name":"second","action_type":"http_mock","action_payload":{"status":200}}
		]
	}`)
	createResponse := performRequest(router, http.MethodPost, "/api/tasks", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", createResponse.Code, createResponse.Body.String())
	}

	var created domain.Task
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	t.Cleanup(func() {
		db.Where("task_id = ?", created.ID).Delete(&domain.TaskStep{})
		db.Delete(&domain.Task{}, created.ID)
	})

	listResponse := performRequest(router, http.MethodGet, "/api/tasks", nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResponse.Code)
	}
	var listed []map[string]json.RawMessage
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	for _, task := range listed {
		var id uint64
		if err := json.Unmarshal(task["id"], &id); err == nil && id == created.ID {
			if _, hasSteps := task["steps"]; hasSteps {
				t.Fatal("list response unexpectedly includes steps")
			}
		}
	}

	detailResponse := performRequest(router, http.MethodGet, "/api/tasks/"+strconv.FormatUint(created.ID, 10), nil)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", detailResponse.Code)
	}
	var detail domain.Task
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode task detail: %v", err)
	}
	if len(detail.Steps) != 2 || detail.Steps[0].StepOrder != 1 || detail.Steps[1].StepOrder != 2 {
		t.Fatalf("detail steps are not in business order: %#v", detail.Steps)
	}

	notFoundResponse := performRequest(router, http.MethodGet, "/api/tasks/18446744073709551615", nil)
	if notFoundResponse.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", notFoundResponse.Code)
	}
}

func performRequest(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
