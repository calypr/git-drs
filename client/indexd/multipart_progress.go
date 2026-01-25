package indexd_client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	dataClient "github.com/calypr/data-client/client/client"
	"github.com/calypr/data-client/client/common"
	req "github.com/calypr/data-client/client/request"
	"github.com/calypr/data-client/client/upload"
)

type progressReader struct {
	reader io.Reader
	report func(int64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 && p.report != nil {
		p.report(int64(n))
	}
	return n, err
}

func multipartUploadWithProgress(ctx context.Context, g3 dataClient.Gen3Interface, request common.FileUploadRequestObject, file *os.File, reportBytes func(int64)) error {
	g3.Logger().Printf("File Upload Request: %#v\n", request)

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat file: %w", err)
	}

	fileSize := stat.Size()
	if fileSize == 0 {
		return fmt.Errorf("file is empty: %s", request.Filename)
	}

	uploadID, finalGUID, err := initMultipartUpload(ctx, g3, request, request.Bucket)
	if err != nil {
		return fmt.Errorf("failed to initiate multipart upload: %w", err)
	}

	key := fmt.Sprintf("%s/%s", finalGUID, request.Filename)
	g3.Logger().Printf("Initialized Upload: ID=%s, Key=%s\n", uploadID, key)

	optimalChunkSize := func(fSize int64) int64 {
		if fSize <= 512*common.MB {
			return 32 * common.MB
		}
		chunkSize := fSize / common.MaxMultipartParts
		if chunkSize < common.MinChunkSize {
			chunkSize = common.MinChunkSize
		}
		return ((chunkSize + common.MB - 1) / common.MB) * common.MB
	}

	chunkSize := optimalChunkSize(fileSize)
	numChunks := int((fileSize + chunkSize - 1) / chunkSize)

	chunks := make(chan int, numChunks)
	for i := 1; i <= numChunks; i++ {
		chunks <- i
	}
	close(chunks)

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		parts        []upload.MultipartPartObject
		uploadErrors []error
	)

	httpClient := &http.Client{Transport: http.DefaultTransport}

	worker := func() {
		defer wg.Done()

		for partNum := range chunks {
			offset := int64(partNum-1) * chunkSize
			size := chunkSize
			if offset+size > fileSize {
				size = fileSize - offset
			}

			section := io.NewSectionReader(file, offset, size)

			url, err := generateMultipartPresignedURL(ctx, g3, key, uploadID, partNum, request.Bucket)
			if err != nil {
				mu.Lock()
				uploadErrors = append(uploadErrors, fmt.Errorf("URL generation failed part %d: %w", partNum, err))
				mu.Unlock()
				return
			}

			reader := &progressReader{reader: section, report: reportBytes}
			etag, err := uploadPartWithClient(ctx, httpClient, url, reader, size)
			if err != nil {
				mu.Lock()
				uploadErrors = append(uploadErrors, fmt.Errorf("upload failed part %d: %w", partNum, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			parts = append(parts, upload.MultipartPartObject{
				PartNumber: partNum,
				ETag:       etag,
			})
			mu.Unlock()
		}
	}

	for range common.MaxConcurrentUploads {
		wg.Add(1)
		go worker()
	}
	wg.Wait()

	if len(uploadErrors) > 0 {
		return fmt.Errorf("multipart upload failed with %d errors: %v", len(uploadErrors), uploadErrors)
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	if err := completeMultipartUpload(ctx, g3, key, uploadID, parts, request.Bucket); err != nil {
		return fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	g3.Logger().Printf("Successfully uploaded %s to %s", request.Filename, key)
	g3.Logger().Succeeded(request.FilePath, request.GUID)
	return nil
}

func initMultipartUpload(ctx context.Context, g3 dataClient.Gen3Interface, request common.FileUploadRequestObject, bucketName string) (string, string, error) {
	reader, err := common.ToJSONReader(
		upload.InitRequestObject{
			Filename: request.Filename,
			Bucket:   bucketName,
			GUID:     request.GUID,
		},
	)
	if err != nil {
		return "", "", err
	}

	cred := g3.GetCredential()
	resp, err := g3.Do(
		ctx,
		&req.RequestBuilder{
			Method:  http.MethodPost,
			Url:     cred.APIEndpoint + common.FenceDataMultipartInitEndpoint,
			Headers: map[string]string{common.HeaderContentType: common.MIMEApplicationJSON},
			Body:    reader,
			Token:   cred.AccessToken,
		},
	)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return "", "", errors.New(err.Error() + "\nPlease check to ensure FENCE version is at 2.8.0 or beyond")
		}
		return "", "", errors.New("Error has occurred during multipart upload initialization, detailed error message: " + err.Error())
	}

	msg, err := g3.ParseFenceURLResponse(resp)
	if err != nil {
		return "", "", errors.New("Error has occurred during multipart upload initialization, detailed error message: " + err.Error())
	}

	if msg.UploadID == "" || msg.GUID == "" {
		return "", "", errors.New("unknown error has occurred during multipart upload initialization. Please check logs from Gen3 services")
	}
	return msg.UploadID, msg.GUID, err
}

func generateMultipartPresignedURL(ctx context.Context, g3 dataClient.Gen3Interface, key string, uploadID string, partNumber int, bucketName string) (string, error) {
	reader, err := common.ToJSONReader(
		upload.MultipartUploadRequestObject{
			Key:        key,
			UploadID:   uploadID,
			PartNumber: partNumber,
			Bucket:     bucketName,
		},
	)
	if err != nil {
		return "", err
	}

	cred := g3.GetCredential()
	resp, err := g3.Do(
		ctx,
		&req.RequestBuilder{
			Url:     cred.APIEndpoint + common.FenceDataMultipartUploadEndpoint,
			Headers: map[string]string{common.HeaderContentType: common.MIMEApplicationJSON},
			Method:  http.MethodPost,
			Body:    reader,
			Token:   cred.AccessToken,
		},
	)
	if err != nil {
		return "", errors.New("Error has occurred during multipart upload presigned url generation, detailed error message: " + err.Error())
	}

	msg, err := g3.ParseFenceURLResponse(resp)
	if err != nil {
		return "", errors.New("Error has occurred during multipart upload initialization, detailed error message: " + err.Error())
	}

	if msg.PresignedURL == "" {
		return "", errors.New("unknown error has occurred during multipart upload presigned url generation. Please check logs from Gen3 services")
	}
	return msg.PresignedURL, err
}

func completeMultipartUpload(ctx context.Context, g3 dataClient.Gen3Interface, key string, uploadID string, parts []upload.MultipartPartObject, bucketName string) error {
	multipartCompleteObject := upload.MultipartCompleteRequestObject{Key: key, UploadID: uploadID, Parts: parts, Bucket: bucketName}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(multipartCompleteObject)
	if err != nil {
		return errors.New("Error occurred during encoding multipart upload data: " + err.Error())
	}

	cred := g3.GetCredential()
	_, err = g3.Do(
		ctx,
		&req.RequestBuilder{
			Url:     cred.APIEndpoint + common.FenceDataMultipartCompleteEndpoint,
			Headers: map[string]string{common.HeaderContentType: common.MIMEApplicationJSON},
			Body:    &buf,
			Method:  http.MethodPost,
			Token:   cred.AccessToken,
		},
	)
	if err != nil {
		return errors.New("Error has occurred during completing multipart upload, detailed error message: " + err.Error())
	}
	return nil
}

func uploadPartWithClient(ctx context.Context, httpClient *http.Client, url string, data io.Reader, partSize int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, data)
	if err != nil {
		return "", err
	}

	req.ContentLength = partSize

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("upload failed (%d): %s", resp.StatusCode, body)
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", errors.New("no ETag returned")
	}

	return strings.Trim(etag, `"`), nil
}
