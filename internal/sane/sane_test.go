package sane_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/sane"
)

const scanimageLFixture = `device ` + "`" + `escl:http://192.168.0.246:80' is a Brother HL-L2395DW series adf scanner
device ` + "`" + `airscan:e0:Brother HL-L2395DW series' is a eSCL Brother HL-L2395DW series ip=192.168.0.246
device ` + "`" + `genesys:libusb:001:004' is a Canon LiDE 110 flatbed scanner
`

const scanimageHelpFlatbedFixture = `Options specific to device ` + "`" + `airscan:e0:Brother HL-L2395DW series':
  Standard:
    --resolution 100|200|300|600dpi [300]
        Sets the resolution of the scanned image.
    --mode Color|Gray [Color]
        Selects the scan mode (e.g., lineart, monochrome, or color).
    --source Flatbed [Flatbed]
        Selects the scan source.
`

const scanimageHelpADFFixture = `Options specific to device ` + "`" + `escl:http://192.168.0.246:80':
  Standard:
    --resolution 150|300|600|1200dpi [150]
        Sets the resolution of the scanned image.
    --mode Color|Gray|Lineart [Gray]
        Selects the scan mode.
    --source Flatbed|ADF [Flatbed]
        Selects the scan source.
`

func TestParseDevices(t *testing.T) {
	devices, err := sane.ParseDevices(scanimageLFixture)
	require.NoError(t, err)
	require.Len(t, devices, 3)

	assert.Equal(t, "escl:http://192.168.0.246:80", devices[0].Device)
	assert.Equal(t, "Brother", devices[0].Vendor)
	assert.Equal(t, "HL-L2395DW series", devices[0].Model)
	assert.Equal(t, "adf scanner", devices[0].Type)

	assert.Equal(t, "airscan:e0:Brother HL-L2395DW series", devices[1].Device)
	assert.Equal(t, "eSCL", devices[1].Vendor)

	assert.Equal(t, "genesys:libusb:001:004", devices[2].Device)
	assert.Equal(t, "Canon", devices[2].Vendor)
	assert.Equal(t, "LiDE 110", devices[2].Model)
	assert.Equal(t, "flatbed scanner", devices[2].Type)
}

func TestParseCapabilities(t *testing.T) {
	t.Run("flatbed only scanner", func(t *testing.T) {
		caps := sane.ParseCapabilities(scanimageHelpFlatbedFixture)
		assert.Equal(t, []int{100, 200, 300, 600}, caps.Resolutions)
		assert.Equal(t, 300, caps.DefaultResolution)
		assert.Equal(t, []string{"Flatbed"}, caps.Sources)
		assert.Equal(t, "Flatbed", caps.DefaultSource)
		assert.False(t, caps.HasADF)
		assert.Equal(t, []string{"Color", "Gray"}, caps.Modes)
		assert.Equal(t, "Color", caps.DefaultMode)
	})

	t.Run("ADF equipped scanner", func(t *testing.T) {
		caps := sane.ParseCapabilities(scanimageHelpADFFixture)
		assert.Equal(t, []int{150, 300, 600, 1200}, caps.Resolutions)
		assert.Equal(t, 150, caps.DefaultResolution)
		assert.Equal(t, []string{"Flatbed", "ADF"}, caps.Sources)
		assert.Equal(t, "Flatbed", caps.DefaultSource)
		assert.True(t, caps.HasADF)
		assert.Equal(t, []string{"Color", "Gray", "Lineart"}, caps.Modes)
		assert.Equal(t, "Gray", caps.DefaultMode)
	})
}

func TestBuildScanArgs(t *testing.T) {
	tests := []struct {
		name     string
		req      sane.ScanRequest
		expected []string
	}{
		{
			name: "full options",
			req: sane.ScanRequest{
				Device:     "airscan:e0:Brother",
				Resolution: 300,
				Source:     "Flatbed",
				Mode:       "Color",
				Format:     "pdf",
				OutputPath: "/tmp/scans/test.pdf",
			},
			expected: []string{
				"-d", "airscan:e0:Brother",
				"--resolution", "300",
				"--source", "Flatbed",
				"--mode", "Color",
				"--format=pdf",
				"-o", "/tmp/scans/test.pdf",
			},
		},
		{
			name: "minimal options",
			req: sane.ScanRequest{
				Device:     "genesys:libusb",
				Resolution: 150,
			},
			expected: []string{
				"-d", "genesys:libusb",
				"--resolution", "150",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := sane.BuildScanArgs(tc.req)
			assert.Equal(t, tc.expected, args)
		})
	}
}

func TestFilenameGeneration(t *testing.T) {
	fixedTime := time.Date(2026, 11, 10, 15, 4, 5, 0, time.UTC)
	name := sane.GenerateFilename("pdf", fixedTime)
	assert.Equal(t, "scan_20261110_150405.pdf", name)

	path := sane.BuildDefaultOutputPath("/home/user/Scans", ".png", fixedTime)
	assert.Equal(t, "/home/user/Scans/scan_20261110_150405.png", path)
}

func TestClient_IsolatedExecution(t *testing.T) {
	ctx := context.Background()
	mock := runner.NewMockRunner()
	client := sane.NewClient(mock)

	mock.Register("scanimage -L", []byte(scanimageLFixture), nil)
	mock.Register("scanimage --help -d airscan:e0:Brother HL-L2395DW series", []byte(scanimageHelpFlatbedFixture), nil)
	mock.Register("scanimage -d airscan:e0:Brother --resolution 300 --source Flatbed --format=pdf -o /tmp/scan.pdf", []byte(""), nil)

	t.Run("GetDevices", func(t *testing.T) {
		devices, err := client.GetDevices(ctx)
		require.NoError(t, err)
		require.Len(t, devices, 3)
		assert.Equal(t, "Brother", devices[0].Vendor)
	})

	t.Run("GetCapabilities", func(t *testing.T) {
		caps, err := client.GetCapabilities(ctx, "airscan:e0:Brother HL-L2395DW series")
		require.NoError(t, err)
		assert.Equal(t, 300, caps.DefaultResolution)
		assert.False(t, caps.HasADF)
	})

	t.Run("Scan execution", func(t *testing.T) {
		err := client.Scan(ctx, sane.ScanRequest{
			Device:     "airscan:e0:Brother",
			Resolution: 300,
			Source:     "Flatbed",
			Format:     "pdf",
			OutputPath: "/tmp/scan.pdf",
		})
		require.NoError(t, err)
	})

	t.Run("Scan error propagation", func(t *testing.T) {
		errMock := runner.NewMockRunner()
		errMock.Register("scanimage -d bad_dev", nil, errors.New("device busy"))
		errClient := sane.NewClient(errMock)

		err := errClient.Scan(ctx, sane.ScanRequest{Device: "bad_dev"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scanimage failed")
	})
}
