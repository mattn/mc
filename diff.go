/*
 * Minio Client (C) 2015 Minio, Inc.
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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/minio/mc/pkg/client"
	"github.com/minio/mc/pkg/console"
	"github.com/minio/minio/pkg/probe"
	"github.com/tchap/go-patricia/patricia"
)

//
//   NOTE: All the parse rules should reduced to 1: Diff(First, Second).
//
//   Valid cases
//   =======================
//   1: diff(f, f) -> diff(f, f)
//   2: diff(f, d) -> copy(f, d/f) -> 1
//   3: diff(d1..., d2) -> []diff(d1/f, d2/f) -> []1
//
//   InValid cases
//   =======================
//   1. diff(d1..., d2) -> INVALID
//   2. diff(d1..., d2...) -> INVALID
//

// DiffMessage json container for diff messages
type DiffMessage struct {
	FirstURL  string       `json:"first"`
	SecondURL string       `json:"second"`
	Diff      string       `json:"diff"`
	Error     *probe.Error `json:"error,omitempty"`
}

func (d DiffMessage) String() string {
	if !globalJSONFlag {
		msg := ""
		switch d.Diff {
		case "only-in-first":
			msg = console.Colorize("DiffMessage", "‘"+d.FirstURL+"’"+" and "+"‘"+d.SecondURL+"’") + console.Colorize("DiffOnlyInFirst", " - only in first.")
		case "type":
			msg = console.Colorize("DiffMessage", "‘"+d.FirstURL+"’"+" and "+"‘"+d.SecondURL+"’") + console.Colorize("DiffType", " - differ in type.")
		case "size":
			msg = console.Colorize("DiffMessage", "‘"+d.FirstURL+"’"+" and "+"‘"+d.SecondURL+"’") + console.Colorize("DiffSize", " - differ in size.")
		default:
			fatalIf(errDummy().Trace(), "Unhandled difference between ‘"+d.FirstURL+"’ and ‘"+d.SecondURL+"’.")
		}
		return msg
	}
	diffJSONBytes, err := json.Marshal(d)
	fatalIf(probe.NewError(err), "Unable to marshal diff message ‘"+d.FirstURL+"’, ‘"+d.SecondURL+"’ and ‘"+d.Diff+"’.")

	return string(diffJSONBytes)
}

// urlJoinPath Join a path to existing URL
func urlJoinPath(url1, url2 string) (string, *probe.Error) {
	u1, e := client.Parse(url1)
	if e != nil {
		return "", probe.NewError(e)
	}
	u2, e := client.Parse(url2)
	if e != nil {
		return "", probe.NewError(e)
	}
	u1.Path = filepath.Join(u1.Path, u2.Path)
	return u1.String(), nil
}

func doDiffInRoutine(firstURL, secondURL string, recursive bool, ch chan DiffMessage) {
	defer close(ch)
	firstClnt, firstContent, err := url2Stat(firstURL)
	if err != nil {
		ch <- DiffMessage{
			Error: err.Trace(firstURL),
		}
		return
	}
	secondClnt, secondContent, err := url2Stat(secondURL)
	if err != nil {
		ch <- DiffMessage{
			Error: err.Trace(secondURL),
		}
		return
	}
	if firstContent.Type.IsRegular() {
		switch {
		case secondContent.Type.IsDir():
			newSecondURL, err := urlJoinPath(secondURL, firstURL)
			if err != nil {
				ch <- DiffMessage{
					Error: err.Trace(secondURL, firstURL),
				}
				return
			}
			doDiffObjects(firstURL, newSecondURL, ch)
		case !secondContent.Type.IsRegular():
			ch <- DiffMessage{
				FirstURL:  firstURL,
				SecondURL: secondURL,
				Diff:      "type",
			}
			return
		case secondContent.Type.IsRegular():
			doDiffObjects(firstURL, secondURL, ch)
		}
	}
	if firstContent.Type.IsDir() {
		switch {
		case !secondContent.Type.IsDir():
			ch <- DiffMessage{
				FirstURL:  firstURL,
				SecondURL: secondURL,
				Diff:      "type",
			}
			return
		default:
			doDiffDirs(firstClnt, secondClnt, recursive, ch)
		}
	}
}

// doDiffObjects - Diff two object URLs
func doDiffObjects(firstURL, secondURL string, ch chan DiffMessage) {
	_, firstContent, errFirst := url2Stat(firstURL)
	_, secondContent, errSecond := url2Stat(secondURL)

	switch {
	case errFirst != nil && errSecond == nil:
		ch <- DiffMessage{
			Error: errFirst.Trace(firstURL, secondURL),
		}
		return
	case errFirst == nil && errSecond != nil:
		ch <- DiffMessage{
			Error: errSecond.Trace(firstURL, secondURL),
		}
		return
	}
	if firstContent.Name == secondContent.Name {
		return
	}
	switch {
	case firstContent.Type.IsRegular():
		if !secondContent.Type.IsRegular() {
			ch <- DiffMessage{
				FirstURL:  firstURL,
				SecondURL: secondURL,
				Diff:      "type",
			}
		}
	default:
		ch <- DiffMessage{
			Error: errNotAnObject(firstURL).Trace(),
		}
		return
	}

	if firstContent.Size != secondContent.Size {
		ch <- DiffMessage{
			FirstURL:  firstURL,
			SecondURL: secondURL,
			Diff:      "size",
		}
	}
}

func dodiff(firstClnt, secondClnt client.Client, ch chan DiffMessage) {
	for contentCh := range firstClnt.List(false) {
		if contentCh.Err != nil {
			ch <- DiffMessage{
				Error: contentCh.Err.Trace(firstClnt.URL().String()),
			}
			return
		}
		newFirstURL, err := urlJoinPath(firstClnt.URL().String(), contentCh.Content.Name)
		if err != nil {
			ch <- DiffMessage{
				Error: err.Trace(firstClnt.URL().String()),
			}
			return
		}
		newSecondURL, err := urlJoinPath(secondClnt.URL().String(), contentCh.Content.Name)
		if err != nil {
			ch <- DiffMessage{
				Error: err.Trace(secondClnt.URL().String()),
			}
			return
		}
		_, newFirstContent, errFirst := url2Stat(newFirstURL)
		_, newSecondContent, errSecond := url2Stat(newSecondURL)
		switch {
		case errFirst == nil && errSecond != nil:
			ch <- DiffMessage{
				FirstURL:  newFirstURL,
				SecondURL: newSecondURL,
				Diff:      "only-in-first",
			}
			continue
		case errFirst == nil && errSecond == nil:
			switch {
			case newFirstContent.Type.IsDir():
				if !newSecondContent.Type.IsDir() {
					ch <- DiffMessage{
						FirstURL:  newFirstURL,
						SecondURL: newSecondURL,
						Diff:      "type",
					}
				}
				continue
			case newFirstContent.Type.IsRegular():
				if !newSecondContent.Type.IsRegular() {
					ch <- DiffMessage{
						FirstURL:  newFirstURL,
						SecondURL: newSecondURL,
						Diff:      "type",
					}
					continue
				}
				doDiffObjects(newFirstURL, newSecondURL, ch)
			}
		}
	} // End of for-loop
}

func dodiffRecursive(firstClnt, secondClnt client.Client, ch chan DiffMessage) {
	firstTrie := patricia.NewTrie()
	secondTrie := patricia.NewTrie()
	wg := new(sync.WaitGroup)

	type urlAttr struct {
		Size int64
		Type os.FileMode
	}

	wg.Add(1)
	go func(ch chan<- DiffMessage) {
		defer wg.Done()
		for firstContentCh := range firstClnt.List(true) {
			if firstContentCh.Err != nil {
				ch <- DiffMessage{
					Error: firstContentCh.Err.Trace(firstClnt.URL().String()),
				}
				return
			}
			firstTrie.Insert(patricia.Prefix(firstContentCh.Content.Name), urlAttr{firstContentCh.Content.Size, firstContentCh.Content.Type})
		}
	}(ch)
	wg.Add(1)
	go func(ch chan<- DiffMessage) {
		defer wg.Done()
		for secondContentCh := range secondClnt.List(true) {
			if secondContentCh.Err != nil {
				ch <- DiffMessage{
					Error: secondContentCh.Err.Trace(secondClnt.URL().String()),
				}
				return
			}
			secondTrie.Insert(patricia.Prefix(secondContentCh.Content.Name), urlAttr{secondContentCh.Content.Size, secondContentCh.Content.Type})
		}
	}(ch)

	doneCh := make(chan struct{})
	defer close(doneCh)
	go func(doneCh <-chan struct{}) {
		cursorCh := cursorAnimate()
		for {
			select {
			case <-time.Tick(100 * time.Millisecond):
				if !globalQuietFlag && !globalJSONFlag {
					console.PrintC("\r" + "Scanning.. " + string(<-cursorCh))
				}
			case <-doneCh:
				return
			}
		}
	}(doneCh)
	wg.Wait()
	doneCh <- struct{}{}
	if !globalQuietFlag && !globalJSONFlag {
		console.Printf("%c[2K\n", 27)
		console.Printf("%c[A", 27)
	}

	matchNameCh := make(chan string, 10000)
	go func(matchNameCh chan<- string) {
		itemFunc := func(prefix patricia.Prefix, item patricia.Item) error {
			matchNameCh <- string(prefix)
			return nil
		}
		firstTrie.Visit(itemFunc)
		defer close(matchNameCh)
	}(matchNameCh)
	for matchName := range matchNameCh {
		firstURLDelimited := firstClnt.URL().String()[:strings.LastIndex(firstClnt.URL().String(), string(firstClnt.URL().Separator))+1]
		secondURLDelimited := secondClnt.URL().String()[:strings.LastIndex(secondClnt.URL().String(), string(secondClnt.URL().Separator))+1]
		firstURL := firstURLDelimited + matchName
		secondURL := secondURLDelimited + matchName
		if !secondTrie.Match(patricia.Prefix(matchName)) {
			ch <- DiffMessage{
				FirstURL:  firstURL,
				SecondURL: secondURL,
				Diff:      "only-in-first",
			}
		} else {
			firstURLAttr := firstTrie.Get(patricia.Prefix(matchName)).(urlAttr)
			secondURLAttr := secondTrie.Get(patricia.Prefix(matchName)).(urlAttr)

			if firstURLAttr.Type.IsRegular() {
				if !secondURLAttr.Type.IsRegular() {
					ch <- DiffMessage{
						FirstURL:  firstURL,
						SecondURL: secondURL,
						Diff:      "type",
					}
					continue
				}
			}

			if firstURLAttr.Type.IsDir() {
				if !secondURLAttr.Type.IsDir() {
					ch <- DiffMessage{
						FirstURL:  firstURL,
						SecondURL: secondURL,
						Diff:      "type",
					}
					continue
				}
			}

			if firstURLAttr.Size != secondURLAttr.Size {
				ch <- DiffMessage{
					FirstURL:  firstURL,
					SecondURL: secondURL,
					Diff:      "size",
				}
			}
		}
	}
}

// doDiffDirs - Diff two Dir URLs
func doDiffDirs(firstClnt, secondClnt client.Client, recursive bool, ch chan DiffMessage) {
	if recursive {
		dodiffRecursive(firstClnt, secondClnt, ch)
		return
	}
	dodiff(firstClnt, secondClnt, ch)
}
