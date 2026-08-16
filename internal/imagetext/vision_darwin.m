// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
//
// macOS Vision text recognition for internal/imagetext (🎯T403.7.1).
//
// One call recognises one normalised sub-rectangle of one image file and
// returns JSON. Cropping happens here rather than by handing Vision a
// regionOfInterest because the crop is the whole point of the tile pass: a
// region of interest is still recognised against the full image's downsampled
// raster, whereas a cropped CGImage is submitted at its own scale, which is
// what recovers small text.

#import <Foundation/Foundation.h>
#import <Vision/Vision.h>
#import <ImageIO/ImageIO.h>
#import <CoreGraphics/CoreGraphics.h>

// jv_vision_recognize recognises text in the sub-rectangle (rx, ry, rw, rh) of
// the image at cpath, all normalised 0..1 with a TOP-LEFT origin.
//
// Returns a malloc'd JSON string {"lines":[{text,confidence,x,y,w,h}]} whose
// boxes are in Vision's own bottom-left normalised space, relative to the
// cropped region. The caller owns the result and must free it. On failure it
// returns NULL and stores a malloc'd message in *errOut.
char *jv_vision_recognize(const char *cpath, double rx, double ry, double rw,
                          double rh, char **errOut) {
  @autoreleasepool {
    if (cpath == NULL) {
      *errOut = strdup("no image path");
      return NULL;
    }
    NSString *path = [NSString stringWithUTF8String:cpath];
    NSURL *url = [NSURL fileURLWithPath:path];
    CGImageSourceRef src = CGImageSourceCreateWithURL((__bridge CFURLRef)url, NULL);
    if (!src) {
      *errOut = strdup("image source failed");
      return NULL;
    }
    CGImageRef img = CGImageSourceCreateImageAtIndex(src, 0, NULL);
    CFRelease(src);
    if (!img) {
      *errOut = strdup("image decode failed");
      return NULL;
    }

    size_t W = CGImageGetWidth(img), H = CGImageGetHeight(img);
    CGImageRef use = img;
    if (rw > 0 && rh > 0 && (rx > 0 || ry > 0 || rw < 1 || rh < 1)) {
      // CGImage pixel coordinates are top-left origin, matching rx/ry.
      CGRect r = CGRectMake(rx * (double)W, ry * (double)H,
                            rw * (double)W, rh * (double)H);
      CGImageRef crop = CGImageCreateWithImageInRect(img, r);
      if (crop) {
        use = crop;
      }
    }

    VNImageRequestHandler *handler =
        [[VNImageRequestHandler alloc] initWithCGImage:use options:@{}];
    VNRecognizeTextRequest *req = [[VNRecognizeTextRequest alloc] init];
    req.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
    req.usesLanguageCorrection = YES;

    NSError *err = nil;
    BOOL ok = [handler performRequests:@[ req ] error:&err];
    NSMutableArray *out = [NSMutableArray array];
    if (ok) {
      for (VNRecognizedTextObservation *ob in req.results) {
        VNRecognizedText *t = [[ob topCandidates:1] firstObject];
        if (!t) continue;
        [out addObject:@{
          @"text" : t.string,
          @"confidence" : @(t.confidence),
          @"x" : @(ob.boundingBox.origin.x),
          @"y" : @(ob.boundingBox.origin.y),
          @"w" : @(ob.boundingBox.size.width),
          @"h" : @(ob.boundingBox.size.height)
        }];
      }
    }
    if (use != img) CGImageRelease(use);
    CGImageRelease(img);

    if (!ok) {
      const char *msg = [[err localizedDescription] UTF8String];
      *errOut = strdup(msg ? msg : "vision request failed");
      return NULL;
    }
    NSData *json = [NSJSONSerialization dataWithJSONObject:@{@"lines" : out}
                                                   options:0
                                                     error:nil];
    if (!json) {
      *errOut = strdup("vision result encode failed");
      return NULL;
    }
    NSString *s = [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding];
    return strdup([s UTF8String]);
  }
}
