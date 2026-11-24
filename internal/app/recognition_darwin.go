//go:build darwin

package app

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var visionQR = sync.OnceValues(func() (objc.ID, error) {
	const framework = "/System/Library/Frameworks/Vision.framework/Vision"
	library, err := purego.Dlopen(framework, purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return 0, errors.New("无法载入 macOS Vision 框架")
	}
	symbol, err := purego.Dlsym(library, "VNBarcodeSymbologyQR")
	if err != nil || symbol == 0 {
		return 0, errors.New("macOS Vision 二维码识别不可用")
	}
	return **(**objc.ID)(unsafe.Pointer(&symbol)), nil
})

func visionAutorelease() func() {
	runtime.LockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	return func() {
		pool.Send(objc.RegisterName("drain"))
		runtime.UnlockOSThread()
	}
}

func recognizeWithVision(ctx context.Context, kind string, languages []string, data []byte) ([]string, error) {
	qr, err := visionQR()
	if err != nil {
		return nil, err
	}
	releasePool := visionAutorelease()
	defer releasePool()
	newObject := objc.RegisterName("new")
	releaseObject := objc.RegisterName("release")
	addObject := objc.RegisterName("addObject:")
	array := objc.ID(objc.GetClass("NSMutableArray"))
	arrayOfOptions := array.Send(objc.RegisterName("array"))
	if kind == "ocr" {
		stringClass := objc.ID(objc.GetClass("NSString"))
		for _, language := range languages {
			value := stringClass.Send(objc.RegisterName("stringWithUTF8String:"), language)
			arrayOfOptions.Send(addObject, value)
		}
	} else {
		arrayOfOptions.Send(addObject, qr)
	}

	var request objc.ID
	if kind == "ocr" {
		const accurateRecognitionLevel = 0
		const multilingualRecognitionRevision = 3
		request = objc.ID(objc.GetClass("VNRecognizeTextRequest")).Send(newObject)
		request.Send(objc.RegisterName("setRecognitionLevel:"), accurateRecognitionLevel)
		request.Send(objc.RegisterName("setRevision:"), multilingualRecognitionRevision)
		request.Send(objc.RegisterName("setRecognitionLanguages:"), arrayOfOptions)
	} else {
		request = objc.ID(objc.GetClass("VNDetectBarcodesRequest")).Send(newObject)
		request.Send(objc.RegisterName("setSymbologies:"), arrayOfOptions)
	}
	if request == 0 {
		return nil, errors.New("无法初始化 macOS Vision 识别")
	}
	defer request.Send(releaseObject)

	imageData := objc.ID(objc.GetClass("NSData")).Send(objc.RegisterName("dataWithBytes:length:"), unsafe.Pointer(unsafe.SliceData(data)), len(data))
	runtime.KeepAlive(data)
	options := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionary"))
	handler := objc.ID(objc.GetClass("VNImageRequestHandler")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithData:options:"), imageData, options)
	if handler == 0 {
		return nil, errors.New("macOS Vision 无法读取截图")
	}
	defer handler.Send(releaseObject)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requests := array.Send(objc.RegisterName("arrayWithObject:"), request)
	cancelled := make(chan struct{})
	stopCancellation := context.AfterFunc(ctx, func() {
		defer close(cancelled)
		releasePool := visionAutorelease()
		defer releasePool()
		request.Send(objc.RegisterName("cancel"))
	})
	var visionError objc.ID
	succeeded := objc.Send[bool](handler, objc.RegisterName("performRequests:error:"), requests, unsafe.Pointer(&visionError))
	if !stopCancellation() {
		<-cancelled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !succeeded {
		if message := visionString(visionError.Send(objc.RegisterName("localizedDescription"))); message != "" {
			return nil, errors.New(message)
		}
		return nil, errors.New("macOS Vision 识别失败")
	}
	return visionValues(request.Send(objc.RegisterName("results")), kind), nil
}

func visionValues(results objc.ID, kind string) []string {
	count := int(results.Send(objc.RegisterName("count")))
	atIndex := objc.RegisterName("objectAtIndex:")
	bestCandidates := objc.RegisterName("topCandidates:")
	firstObject := objc.RegisterName("firstObject")
	textValue := objc.RegisterName("string")
	qrValue := objc.RegisterName("payloadStringValue")
	values := make([]string, 0, count)
	for index := range count {
		observation := results.Send(atIndex, index)
		var value objc.ID
		if kind == "ocr" {
			value = observation.Send(bestCandidates, 1).Send(firstObject).Send(textValue)
		} else {
			value = observation.Send(qrValue)
		}
		if text := visionString(value); text != "" {
			if kind == "qr" || strings.TrimSpace(text) != "" {
				values = append(values, text)
			}
		}
	}
	return values
}

func visionString(value objc.ID) string {
	const utf8 = 4
	size := int(value.Send(objc.RegisterName("lengthOfBytesUsingEncoding:"), utf8))
	if size <= 0 {
		return ""
	}
	data := objc.Send[unsafe.Pointer](value, objc.RegisterName("UTF8String"))
	if data == nil {
		return ""
	}
	return string(unsafe.Slice((*byte)(data), size))
}
