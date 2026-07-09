package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/goharbor/acceleration-service/pkg/model"
	"github.com/goharbor/acceleration-service/pkg/task"
	"github.com/pkg/errors"
)

func (client *Client) CreateTask(src string, sync bool) (*model.CreateTaskResponse, error) {
	payload := model.Payload{
		Type: model.TopicPushArtifact,
		EventData: &model.EventData{
			Resources: []*model.Resource{
				{
					ResourceURL: src,
				},
			},
		},
	}

	data, err := marshal(payload)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v1/conversions?sync=%s", strconv.FormatBool(sync))
	resp, err := client.Request(http.MethodPost, path, data, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result model.CreateTaskResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return &result, nil
}

func (client *Client) GetTask(id string) (*task.Task, error) {
	path := fmt.Sprintf("/api/v1/conversions/%s", id)
	resp, err := client.Request(http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("task %s not found", id)
	}

	var t task.Task
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&t); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return &t, nil
}

func (client *Client) ListTask() ([]task.Task, error) {
	resp, err := client.Request(http.MethodGet, "/api/v1/conversions", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tasks []task.Task
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&tasks); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return tasks, nil
}
