package cups_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/cups"
	"github.com/tkilb/lazyprint/internal/runner"
)

// Fixture captured directly from real Brother HL-L2395DW system output.
const brotherLpstatFixture = `printer Brother_Printer is idle.  enabled since Tue 10 Nov 2026 12:28:52 PM EST
	Waiting for job to complete.
system default destination: Brother_Printer
`

const brotherDeviceFixture = `device for Brother_Printer: ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/
`

const multiPrinterFixture = `printer Brother_Printer is idle.  enabled since Tue 10 Nov 2026 12:28:52 PM EST
	Waiting for job to complete.
printer HP_LaserJet now printing HP_LaserJet-101.  enabled since Mon 09 Nov 2026 08:15:00 AM EST
	Processing document
printer Zebra_Label is disabled since Sun 08 Nov 2026 04:00:00 PM EST
	Out of paper
system default destination: Brother_Printer
`

const jobsFixture = `Brother_Printer-42      tylerkilburn     1024   Tue 10 Nov 2026 12:30:00 PM EST
HP_LaserJet-101         alice            84320  Tue 10 Nov 2026 12:35:10 PM EST
`

const optionsFixture = `PageSize/Media Size: 215x345mm 3x5 A4 A5 A6 Env10 EnvC5 EnvDL EnvMonarch Executive FanFoldGermanLegal ISOB5 Legal *Letter Oficio Custom.WIDTHxHEIGHT
InputSlot/Media Source: *Auto Manual Tray1
cupsPrintQuality/cupsPrintQuality: Draft *Normal High
ColorModel/Output Mode: *Gray
Duplex/Duplex: *None DuplexNoTumble DuplexTumble
`

func TestParsePrinters(t *testing.T) {
	t.Run("single brother printer", func(t *testing.T) {
		printers, defaultDest, err := cups.ParsePrinters(brotherLpstatFixture)
		require.NoError(t, err)
		assert.Equal(t, "Brother_Printer", defaultDest)
		require.Len(t, printers, 1)

		p := printers[0]
		assert.Equal(t, "Brother_Printer", p.Name)
		assert.Equal(t, cups.StatusIdle, p.Status)
		assert.True(t, p.Enabled)
		assert.True(t, p.IsDefault)
		assert.Equal(t, "Tue 10 Nov 2026 12:28:52 PM EST", p.Since)
		assert.Equal(t, "Waiting for job to complete.", p.StatusMessage)
	})

	t.Run("multi printer fixture with diverse statuses", func(t *testing.T) {
		printers, defaultDest, err := cups.ParsePrinters(multiPrinterFixture)
		require.NoError(t, err)
		assert.Equal(t, "Brother_Printer", defaultDest)
		require.Len(t, printers, 3)

		assert.Equal(t, "Brother_Printer", printers[0].Name)
		assert.Equal(t, cups.StatusIdle, printers[0].Status)
		assert.True(t, printers[0].IsDefault)

		assert.Equal(t, "HP_LaserJet", printers[1].Name)
		assert.Equal(t, cups.StatusProcessing, printers[1].Status)
		assert.False(t, printers[1].IsDefault)
		assert.Equal(t, "Processing document", printers[1].StatusMessage)

		assert.Equal(t, "Zebra_Label", printers[2].Name)
		assert.Equal(t, cups.StatusDisabled, printers[2].Status)
		assert.False(t, printers[2].Enabled)
		assert.Equal(t, "Out of paper", printers[2].StatusMessage)
	})

	t.Run("empty output", func(t *testing.T) {
		printers, defaultDest, err := cups.ParsePrinters("")
		require.NoError(t, err)
		assert.Empty(t, defaultDest)
		assert.Empty(t, printers)
	})
}

func TestParseDeviceURIs(t *testing.T) {
	uris := cups.ParseDeviceURIs(brotherDeviceFixture)
	assert.Equal(t, "ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/", uris["Brother_Printer"])
}

func TestParseJobs(t *testing.T) {
	t.Run("multiple active jobs", func(t *testing.T) {
		jobs, err := cups.ParseJobs(jobsFixture)
		require.NoError(t, err)
		require.Len(t, jobs, 2)

		assert.Equal(t, "Brother_Printer-42", jobs[0].ID)
		assert.Equal(t, "Brother_Printer", jobs[0].Printer)
		assert.Equal(t, 42, jobs[0].JobNumber)
		assert.Equal(t, "tylerkilburn", jobs[0].User)
		assert.Equal(t, int64(1024), jobs[0].Size)
		assert.Equal(t, "Tue 10 Nov 2026 12:30:00 PM EST", jobs[0].SubmittedAt)

		assert.Equal(t, "HP_LaserJet-101", jobs[1].ID)
		assert.Equal(t, "HP_LaserJet", jobs[1].Printer)
		assert.Equal(t, 101, jobs[1].JobNumber)
		assert.Equal(t, "alice", jobs[1].User)
		assert.Equal(t, int64(84320), jobs[1].Size)
	})

	t.Run("empty queue", func(t *testing.T) {
		jobs, err := cups.ParseJobs("")
		require.NoError(t, err)
		assert.Empty(t, jobs)
	})
}

func TestParseOptions(t *testing.T) {
	options, err := cups.ParseOptions(optionsFixture)
	require.NoError(t, err)
	require.Len(t, options, 5)

	pageSize := options[0]
	assert.Equal(t, "PageSize", pageSize.Name)
	assert.Equal(t, "Media Size", pageSize.Description)
	assert.Equal(t, "Letter", pageSize.Default)
	assert.Contains(t, pageSize.Choices, "A4")
	assert.Contains(t, pageSize.Choices, "Letter")

	duplex := options[4]
	assert.Equal(t, "Duplex", duplex.Name)
	assert.Equal(t, "None", duplex.Default)
	assert.Equal(t, []string{"None", "DuplexNoTumble", "DuplexTumble"}, duplex.Choices)
}

func TestClient_IsolatedExecution(t *testing.T) {
	ctx := context.Background()
	mock := runner.NewMockRunner()
	client := cups.NewClient(mock)

	mock.Register("lpstat -p -d", []byte(brotherLpstatFixture), nil)
	mock.Register("lpstat -v", []byte(brotherDeviceFixture), nil)
	mock.Register("lpstat -o", []byte(jobsFixture), nil)
	mock.Register("lpoptions -p Brother_Printer -l", []byte(optionsFixture), nil)
	mock.Register("cancel Brother_Printer-42", []byte(""), nil)
	mock.Register("cancel -a Brother_Printer", []byte(""), nil)
	mock.Register("cupsdisable Brother_Printer", []byte(""), nil)
	mock.Register("cupsenable Brother_Printer", []byte(""), nil)
	mock.Register("lpadmin -x Brother_Printer", []byte(""), nil)
	mock.Register("lpadmin -p Brother_Office -E -v ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/ -m everywhere", []byte(""), nil)
	mock.Register("lpoptions -d Brother_Office", []byte(""), nil)
	mock.Register("lpoptions -d Brother_Printer", []byte(""), nil)

	t.Run("GetPrinters", func(t *testing.T) {
		printers, err := client.GetPrinters(ctx)
		require.NoError(t, err)
		require.Len(t, printers, 1)
		assert.Equal(t, "Brother_Printer", printers[0].Name)
		assert.True(t, printers[0].IsDefault)
		assert.Equal(t, "ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/", printers[0].DeviceURI)
	})

	t.Run("GetJobs", func(t *testing.T) {
		jobs, err := client.GetJobs(ctx)
		require.NoError(t, err)
		require.Len(t, jobs, 2)
		assert.Equal(t, "Brother_Printer-42", jobs[0].ID)
	})

	t.Run("GetOptions", func(t *testing.T) {
		options, err := client.GetOptions(ctx, "Brother_Printer")
		require.NoError(t, err)
		require.Len(t, options, 5)
		assert.Equal(t, "PageSize", options[0].Name)
	})

	t.Run("CancelJob", func(t *testing.T) {
		err := client.CancelJob(ctx, "Brother_Printer-42")
		require.NoError(t, err)
	})

	t.Run("CancelAll", func(t *testing.T) {
		err := client.CancelAll(ctx, "Brother_Printer")
		require.NoError(t, err)
	})

	t.Run("SetDefault", func(t *testing.T) {
		err := client.SetDefault(ctx, "Brother_Printer")
		require.NoError(t, err)
	})

	t.Run("PausePrinter", func(t *testing.T) {
		err := client.PausePrinter(ctx, "Brother_Printer")
		require.NoError(t, err)
	})

	t.Run("ResumePrinter", func(t *testing.T) {
		err := client.ResumePrinter(ctx, "Brother_Printer")
		require.NoError(t, err)
	})

	t.Run("DeletePrinter", func(t *testing.T) {
		err := client.DeletePrinter(ctx, "Brother_Printer")
		require.NoError(t, err)
	})

	t.Run("RenamePrinter", func(t *testing.T) {
		err := client.RenamePrinter(ctx, "Brother_Printer", "Brother_Office", "ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/", true)
		require.NoError(t, err)
	})

	t.Run("Error propagation", func(t *testing.T) {
		mockErr := runner.NewMockRunner()
		mockErr.Register("cancel 999", nil, errors.New("exit status 1"))
		errClient := cups.NewClient(mockErr)

		err := errClient.CancelJob(ctx, "999")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancel 999 failed")
	})
}
