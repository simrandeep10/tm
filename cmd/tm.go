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

package main

import (
	"flag"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/txn2/micro"
	"github.com/txn2/provision"
	"github.com/txn2/tm"
	"go.uber.org/zap"
)

var (
	modeEnv          = getEnv("MODE", "protected")
	elasticServerEnv = getEnv("ELASTIC_SERVER", "http://elasticsearch:9200")
)

func main() {
	mode := flag.String("mode", modeEnv, "Protected or internal modes. (internal = security bypass)")
	esServer := flag.String("esServer", elasticServerEnv, "Elasticsearch Server")

	serverCfg, _ := micro.NewServerCfg("Type Model (tm)")
	server := micro.NewServer(serverCfg)

	tmApi, err := tm.NewApi(&tm.Config{
		Logger:        server.Logger,
		HttpClient:    server.Client,
		ElasticServer: *esServer,
	})
	if err != nil {
		server.Logger.Fatal("failure to instantiate the model API: " + err.Error())
		os.Exit(1)
	}

	accessCheck := func(admin bool) gin.HandlerFunc {
		return provision.AccountAccessCheckHandler(admin)
	}

	server.Logger.Info("Mode status.", zap.String("mode", *mode))

	if *mode == "internal" {
		accessCheck = func(admin bool) gin.HandlerFunc {
			return func(c *gin.Context) {}
		}
	}

	// User token middleware
	if *mode != "internal" {
		server.Router.Use(provision.UserTokenHandler())
	}

	// Get a model
	server.Router.GET("model/:account/:id",
		accessCheck(false),
		tmApi.GetModelHandler,
	)

	// Upsert a model
	server.Router.POST("model/:account",
		accessCheck(true),
		tmApi.UpsertModelHandler,
	)

	// Search Models
	server.Router.POST("searchModels/:account",
		accessCheck(false),
		tmApi.SearchModelsHandler,
	)

	// run provisioning tm
	server.Run()
}

// getEnv gets an environment variable or sets a default if
// one does not exist.
func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}

	return value
}
