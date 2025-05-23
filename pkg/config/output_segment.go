// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/livekit/egress/pkg/errors"
	"github.com/livekit/egress/pkg/types"
	"github.com/livekit/protocol/livekit"
)

type SegmentConfig struct {
	outputConfig

	SegmentsInfo         *livekit.SegmentsInfo
	LocalDir             string
	StorageDir           string
	PlaylistFilename     string
	LivePlaylistFilename string
	SegmentPrefix        string
	SegmentSuffix        livekit.SegmentedFileSuffix
	SegmentDuration      int

	DisableManifest bool
	StorageConfig   *StorageConfig
}

func (p *PipelineConfig) GetSegmentConfig() *SegmentConfig {
	o, ok := p.Outputs[types.EgressTypeSegments]
	if !ok || len(o) == 0 {
		return nil
	}
	return o[0].(*SegmentConfig)
}

// segments should always be added last, so we can check keyframe interval from file/stream
func (p *PipelineConfig) getSegmentConfig(segments *livekit.SegmentedFileOutput) (*SegmentConfig, error) {
	sc, err := p.getStorageConfig(segments)
	if err != nil {
		return nil, err
	}

	conf := &SegmentConfig{
		SegmentsInfo:         &livekit.SegmentsInfo{},
		SegmentPrefix:        clean(segments.FilenamePrefix),
		SegmentSuffix:        segments.FilenameSuffix,
		PlaylistFilename:     clean(segments.PlaylistName),
		LivePlaylistFilename: clean(segments.LivePlaylistName),
		SegmentDuration:      int(segments.SegmentDuration),
		DisableManifest:      segments.DisableManifest,
		StorageConfig:        sc,
	}

	if conf.SegmentDuration == 0 {
		conf.SegmentDuration = 4
	}

	switch segments.Protocol {
	case livekit.SegmentedFileProtocol_DEFAULT_SEGMENTED_FILE_PROTOCOL,
		livekit.SegmentedFileProtocol_HLS_PROTOCOL:
		conf.OutputType = types.OutputTypeHLS
	}

	// filename
	if err = conf.updatePrefixAndPlaylist(p); err != nil {
		return nil, err
	}

	return conf, nil
}

func removeKnownExtension(filename string) string {
	if extIdx := strings.LastIndex(filename, "."); extIdx > -1 {
		existingExt := types.FileExtension(filename[extIdx:])
		if _, ok := types.FileExtensions[existingExt]; ok {
			filename = filename[:extIdx]
		}
		filename = filename[:extIdx]
	}

	return filename
}

func (o *SegmentConfig) updatePrefixAndPlaylist(p *PipelineConfig) error {
	identifier, replacements := p.getFilenameInfo()

	o.SegmentPrefix = stringReplace(o.SegmentPrefix, replacements)
	o.PlaylistFilename = stringReplace(o.PlaylistFilename, replacements)
	o.LivePlaylistFilename = stringReplace(o.LivePlaylistFilename, replacements)

	ext := types.FileExtensionForOutputType[o.OutputType]

	playlistDir, playlistName := path.Split(o.PlaylistFilename)
	livePlaylistDir, livePlaylistName := path.Split(o.LivePlaylistFilename)
	segmentDir, segmentPrefix := path.Split(o.SegmentPrefix)

	// force live playlist to be in the same directory as the main playlist
	if livePlaylistDir != "" && livePlaylistDir != playlistDir {
		return errors.ErrInvalidInput("live_playlist_name must be in same directory as playlist_name")
	}

	// remove extension from playlist name
	playlistName = removeKnownExtension(playlistName)
	livePlaylistName = removeKnownExtension(livePlaylistName)

	// only keep segmentDir if it is a subdirectory of playlistDir
	if segmentDir != "" {
		if playlistDir == segmentDir {
			segmentDir = ""
		} else if playlistDir == "" {
			playlistDir = segmentDir
			segmentDir = ""
		}
	}
	o.StorageDir = playlistDir

	// ensure playlistName
	if playlistName == "" {
		if segmentPrefix != "" {
			playlistName = segmentPrefix
		} else {
			playlistName = fmt.Sprintf("%s-%s", identifier, time.Now().Format("2006-01-02T150405"))
		}
	}
	// live playlist disabled by default

	// ensure filePrefix
	if segmentPrefix == "" {
		segmentPrefix = playlistName
	}

	// update config
	o.StorageDir = playlistDir
	o.PlaylistFilename = fmt.Sprintf("%s%s", playlistName, ext)
	if livePlaylistName != "" {
		o.LivePlaylistFilename = fmt.Sprintf("%s%s", livePlaylistName, ext)
	}
	o.SegmentPrefix = fmt.Sprintf("%s%s", segmentDir, segmentPrefix)

	if o.PlaylistFilename == o.LivePlaylistFilename {
		return errors.ErrInvalidInput("live_playlist_name cannot be identical to playlist_name")
	}

	// Prepend the configuration base directory and the egress Id
	// os.ModeDir creates a directory with mode 000 when mapping the directory outside the container
	o.LocalDir = p.TmpDir
	if segmentDir != "" {
		if err := os.MkdirAll(path.Join(o.LocalDir, segmentDir), 0755); err != nil {
			return err
		}
	}

	o.SegmentsInfo.PlaylistName = path.Join(o.StorageDir, o.PlaylistFilename)
	if o.LivePlaylistFilename != "" {
		o.SegmentsInfo.LivePlaylistName = path.Join(o.StorageDir, o.LivePlaylistFilename)
	}
	return nil
}
