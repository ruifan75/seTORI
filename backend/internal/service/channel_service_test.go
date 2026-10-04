package service

import (
	"testing"

	"github.com/ruifan75/setori/internal/models"
)

func TestToChannelResponseCanEditMetadata(t *testing.T) {
	svc := &ChannelService{}

	holodex := svc.toChannelResponse(models.Channel{
		ID:             "UC_holodex",
		Name:           "Holodex Channel",
		MetadataSource: "holodex",
	})
	if holodex.CanEditMetadata {
		t.Fatal("Holodex sourced singer should not be manually editable")
	}

	youtube := svc.toChannelResponse(models.Channel{
		ID:             "UC_youtube",
		Name:           "YouTube Channel",
		MetadataSource: "youtube",
	})
	if !youtube.CanEditMetadata {
		t.Fatal("YouTube fallback singer should be manually editable")
	}
}
