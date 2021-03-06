// Copyright 2019 txn2
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tm implements Type Models for txn2 projects.
package tm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/txn2/ack"
	"github.com/txn2/es"
	"github.com/txn2/micro"
	"go.uber.org/zap"
)

// ModelResult returned from Elastic
type ModelResult struct {
	es.Result
	Source Model `json:"_source"`
}

// ModelResultAck
type ModelResultAck struct {
	ack.Ack
	Payload ModelResult `json:"payload"`
}

// Config
type Config struct {
	Logger     *zap.Logger
	HttpClient *micro.Client

	// used for communication with Elasticsearch
	// if nil, HttpClient will be used.
	Elastic       *es.Client
	ElasticServer string
}

// Api
type Api struct {
	*Config
}

// NewApi
func NewApi(cfg *Config) (*Api, error) {
	a := &Api{Config: cfg}

	if a.Elastic == nil {
		// Configure an elastic client
		a.Elastic = es.CreateClient(es.Config{
			Log:           cfg.Logger,
			HttpClient:    cfg.HttpClient.Http,
			ElasticServer: cfg.ElasticServer,
		})
	}

	// check for elasticsearch
	backOff := []int{10, 10, 15, 15, 30, 30, 45}
	for _, boff := range backOff {
		code, _, _ := a.Elastic.Get("")
		a.Logger.Info("Attempting to contact Elasticsearch", zap.String("server", a.Elastic.ElasticServer))

		if code == 200 {
			a.Logger.Info("Connection to Elastic search successful.", zap.String("server", a.Elastic.ElasticServer))
			break
		}

		a.Logger.Warn("Unable to contact Elasticsearch rolling back off.", zap.Int("wait_seconds", boff))
		<-time.After(time.Duration(boff) * time.Second)
	}

	// send template mappings for models index
	_, _, err := a.Elastic.SendEsMapping(GetModelsTemplateMapping())
	if err != nil {
		return nil, err
	}

	return a, nil
}

// GetModel
func (a *Api) GetModel(account string, id string) (int, *ModelResult, error) {

	code, ret, err := a.Elastic.Get(fmt.Sprintf("%s-%s/_doc/%s", account, IdxModel, id))
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, nil, err
	}

	modelResult := &ModelResult{}
	err = json.Unmarshal(ret, modelResult)
	if err != nil {
		return code, nil, err
	}

	return code, modelResult, nil
}

// GetModelHandler
func (a *Api) GetModelHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// GetModelHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")
	id := c.Param("id")
	code, modelResult, err := a.GetModel(account, id)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
		ak.GinErrorAbort(500, "EsError", err.Error())
		return
	}

	if code >= 400 && code < 500 {
		ak.SetPayload("Model " + id + " not found.")
		ak.GinErrorAbort(404, "ModelNotFound", "Model not found")
		return
	}

	ak.SetPayloadType("ModelResult")
	ak.GinSend(modelResult)
}

// UpsertModel
func (a *Api) UpsertModel(account string, model *Model) (int, es.Result, error) {
	a.Logger.Info("Upsert model record", zap.String("account", account), zap.String("machine_name", model.MachineName))

	// send template mappings for models index
	code, templateMappingResult, err := a.Elastic.SendEsMapping(MakeModelTemplateMapping(account, model))
	if err != nil {
		return code, templateMappingResult, err
	}

	return a.Elastic.PutObj(fmt.Sprintf("%s-%s/_doc/%s", account, IdxModel, model.MachineName), model)
}

// UpsertModelHandler
func (a *Api) UpsertModelHandler(c *gin.Context) {
	ak := ack.Gin(c)

	// UpsertModelHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")

	model := &Model{}
	err := ak.UnmarshalPostAbort(model)
	if err != nil {
		a.Logger.Error("Upsert failure.", zap.Error(err))
		return
	}

	// ensure lowercase machine name
	model.MachineName = strings.ToLower(model.MachineName)

	//ak.GinSend(MakeModelTemplateMapping(account, model))
	//return

	code, esResult, err := a.UpsertModel(account, model)
	if err != nil {
		a.Logger.Error("Upsert failure.", zap.Error(err))
		ak.SetPayloadType("ErrorMessage")
		ak.SetPayload("there was a problem upserting the model")
		ak.GinErrorAbort(500, "UpsertError", err.Error())
		return
	}

	if code < 200 || code >= 300 {
		a.Logger.Error("Es returned a non 200")
		ak.SetPayloadType("EsError")
		ak.SetPayload(esResult)
		ak.GinErrorAbort(500, "EsError", "Es returned a non 200")
		return
	}

	ak.SetPayloadType("EsResult")
	ak.GinSend(esResult)
}
