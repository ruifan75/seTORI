package service

import (
	"testing"

	"github.com/ruifan75/setori/internal/models"
)

func TestToSingerResponseCanEditMetadata(t *testing.T) {
	svc := &SingerService{}

	holodex := svc.toSingerResponse(models.Singer{
		ID:             "UC_holodex",
		Name:           "Holodex Channel",
		MetadataSource: "holodex",
	})
	if holodex.CanEditMetadata {
		t.Fatal("Holodex sourced singer should not be manually editable")
	}

	youtube := svc.toSingerResponse(models.Singer{
		ID:             "UC_youtube",
		Name:           "YouTube Channel",
		MetadataSource: "youtube",
	})
	if !youtube.CanEditMetadata {
		t.Fatal("YouTube fallback singer should be manually editable")
	}
}
