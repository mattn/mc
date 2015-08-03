/*
 * Minio Client (C) 2014, 2015 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"

	"github.com/minio/cli"
	"github.com/minio/mc/pkg/console"
	"github.com/minio/minio/pkg/probe"
)

// Help message.
var mbCmd = cli.Command{
	Name:   "mb",
	Usage:  "Make a bucket or folder",
	Action: runMakeBucketCmd,
	CustomHelpTemplate: `NAME:
   mc {{.Name}} - {{.Usage}}

USAGE:
   mc {{.Name}} TARGET [TARGET...] {{if .Description}}

DESCRIPTION:
   {{.Description}}{{end}}{{if .Flags}}

FLAGS:
   {{range .Flags}}{{.}}
   {{end}}{{ end }}

EXAMPLES:
   1. Create a bucket on Amazon S3 cloud storage.
      $ mc {{.Name}} https://s3.amazonaws.com/public-document-store

   3. Make a folder on local filesystem, including its parent folders as needed.
      $ mc {{.Name}} ~/

   3. Create a bucket on Minio cloud storage.
      $ mc {{.Name}} https://play.minio.io:9000/mongodb-backup
`,
}

// runMakeBucketCmd is the handler for mc mb command
func runMakeBucketCmd(ctx *cli.Context) {
	if !ctx.Args().Present() || ctx.Args().First() == "help" {
		cli.ShowCommandHelpAndExit(ctx, "mb", 1) // last argument is exit code
	}
	config := mustGetMcConfig()
	for _, arg := range ctx.Args() {
		targetURL, err := getExpandedURL(arg, config.Aliases)
		ifFatal(err)
		msg, err := doMakeBucketCmd(targetURL)
		fmt.Println(msg)
		ifFatal(err)
		console.Infoln(msg)
	}
}

// doMakeBucketCmd -
func doMakeBucketCmd(targetURL string) (string, *probe.Error) {
	clnt, err := target2Client(targetURL)
	if err != nil {
		msg := fmt.Sprintf("Unable to initialize client for ‘%s’", targetURL)
		return msg, err.Trace()
	}
	err = clnt.MakeBucket()
	if err != nil {
		msg := fmt.Sprintf("Failed to create bucket for URL ‘%s’", clnt.URL().String())
		return msg, err.Trace()
	}
	return "Bucket created successfully : " + clnt.URL().String(), nil
}