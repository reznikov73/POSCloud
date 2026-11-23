//go:build android

// Package camera открывает системную камеру и сохраняет снимок в общую память.
//
// Java-классов в проекте нет, поэтому всё сделано через JNI: получаем JVM,
// берём контекст приложения, создаём через MediaStore запись для будущего
// снимка (content://-URI), отдаём её камере как EXTRA_OUTPUT и запускаем
// ACTION_IMAGE_CAPTURE. Камера пишет файл сама, а приложение затем читает его
// по обычному пути — файл сразу лежит в папке синхронизации.
package camera

/*
#cgo LDFLAGS: -llog -ldl
#include <jni.h>
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef jint (*jni_get_vms_fn)(JavaVM**, jsize, jsize*);

static void set_err(char* err, int len, const char* msg) {
	if (err && len > 0) {
		snprintf(err, len, "%s", msg);
	}
}

// take_exception превращает висящее JNI-исключение в текст ошибки.
static int take_exception(JNIEnv* env, char* err, int len, const char* where) {
	if (!(*env)->ExceptionCheck(env)) {
		return 0;
	}
	jthrowable th = (*env)->ExceptionOccurred(env);
	(*env)->ExceptionClear(env);
	char msg[256];
	snprintf(msg, sizeof msg, "%s", where);
	if (th) {
		jclass cls = (*env)->GetObjectClass(env, th);
		if (cls) {
			jmethodID toString = (*env)->GetMethodID(env, cls, "toString", "()Ljava/lang/String;");
			if (toString) {
				jstring s = (jstring)(*env)->CallObjectMethod(env, th, toString);
				if (s) {
					const char* utf = (*env)->GetStringUTFChars(env, s, NULL);
					if (utf) {
						snprintf(msg, sizeof msg, "%s: %s", where, utf);
						(*env)->ReleaseStringUTFChars(env, s, utf);
					}
					(*env)->DeleteLocalRef(env, s);
				}
			}
			(*env)->DeleteLocalRef(env, cls);
		}
		(*env)->DeleteLocalRef(env, th);
	}
	set_err(err, len, msg);
	return 1;
}

// start_capture создаёт запись в MediaStore и запускает камеру.
static int start_capture(const char* display_name, const char* relative_path, char* err, int errlen) {
	void* lib = dlopen("libnativehelper.so", RTLD_NOW);
	if (!lib) {
		lib = dlopen("libart.so", RTLD_NOW);
	}
	if (!lib) {
		set_err(err, errlen, "камера недоступна: нет доступа к JVM");
		return 1;
	}
	jni_get_vms_fn get_vms = (jni_get_vms_fn)dlsym(lib, "JNI_GetCreatedJavaVMs");
	if (!get_vms) {
		set_err(err, errlen, "камера недоступна: JNI_GetCreatedJavaVMs не найден");
		return 2;
	}
	JavaVM* vm = NULL;
	jsize count = 0;
	if (get_vms(&vm, 1, &count) != JNI_OK || count < 1 || vm == NULL) {
		set_err(err, errlen, "камера недоступна: JVM не запущена");
		return 3;
	}

	JNIEnv* env = NULL;
	int attached = 0;
	jint res = (*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6);
	if (res == JNI_EDETACHED) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			set_err(err, errlen, "камера недоступна: не удалось подключиться к JVM");
			return 4;
		}
		attached = 1;
	} else if (res != JNI_OK) {
		set_err(err, errlen, "камера недоступна: GetEnv");
		return 5;
	}

	int rc = 0;
	jclass cAT = NULL, cCtx = NULL, cCV = NULL, cMS = NULL, cIntent = NULL;
	jobject app = NULL, resolver = NULL, values = NULL, insertUri = NULL, intent = NULL;
	jstring jName = NULL, jRel = NULL, jAction = NULL, jKey = NULL;

	cAT = (*env)->FindClass(env, "android/app/ActivityThread");
	if (take_exception(env, err, errlen, "ActivityThread")) { rc = 10; goto done; }
	if (!cAT) { set_err(err, errlen, "камера недоступна: ActivityThread"); rc = 10; goto done; }

	{
		jmethodID current = (*env)->GetStaticMethodID(env, cAT, "currentApplication", "()Landroid/app/Application;");
		if (take_exception(env, err, errlen, "currentApplication")) { rc = 11; goto done; }
		app = (*env)->CallStaticObjectMethod(env, cAT, current);
		if (take_exception(env, err, errlen, "currentApplication()")) { rc = 11; goto done; }
		if (!app) { set_err(err, errlen, "камера недоступна: нет контекста приложения"); rc = 11; goto done; }
	}

	cCtx = (*env)->FindClass(env, "android/content/Context");
	{
		jmethodID getResolver = (*env)->GetMethodID(env, cCtx, "getContentResolver", "()Landroid/content/ContentResolver;");
		resolver = (*env)->CallObjectMethod(env, app, getResolver);
		if (take_exception(env, err, errlen, "getContentResolver")) { rc = 12; goto done; }
		jmethodID startAct = (*env)->GetMethodID(env, cCtx, "startActivity", "(Landroid/content/Intent;)V");
		if (!resolver || !startAct) { set_err(err, errlen, "камера недоступна: контекст"); rc = 12; goto done; }
	}

	// ContentValues: имя файла, тип и папка относительно общей памяти.
	cCV = (*env)->FindClass(env, "android/content/ContentValues");
	{
		jmethodID ctor = (*env)->GetMethodID(env, cCV, "<init>", "()V");
		jmethodID put = (*env)->GetMethodID(env, cCV, "put", "(Ljava/lang/String;Ljava/lang/String;)V");
		values = (*env)->NewObject(env, cCV, ctor);
		jName = (*env)->NewStringUTF(env, display_name);
		jRel = (*env)->NewStringUTF(env, relative_path);
		jstring kName = (*env)->NewStringUTF(env, "_display_name");
		jstring kMime = (*env)->NewStringUTF(env, "mime_type");
		jstring kRel = (*env)->NewStringUTF(env, "relative_path");
		jstring vMime = (*env)->NewStringUTF(env, "image/jpeg");
		(*env)->CallVoidMethod(env, values, put, kName, jName);
		(*env)->CallVoidMethod(env, values, put, kMime, vMime);
		(*env)->CallVoidMethod(env, values, put, kRel, jRel);
		if (take_exception(env, err, errlen, "ContentValues")) { rc = 13; goto done; }
	}

	cMS = (*env)->FindClass(env, "android/provider/MediaStore$Images$Media");
	{
		jfieldID fUri = (*env)->GetStaticFieldID(env, cMS, "EXTERNAL_CONTENT_URI", "Landroid/net/Uri;");
		jobject collection = (*env)->GetStaticObjectField(env, cMS, fUri);
		jclass cRes = (*env)->FindClass(env, "android/content/ContentResolver");
		jmethodID insert = (*env)->GetMethodID(env, cRes, "insert",
			"(Landroid/net/Uri;Landroid/content/ContentValues;)Landroid/net/Uri;");
		insertUri = (*env)->CallObjectMethod(env, resolver, insert, collection, values);
		if (take_exception(env, err, errlen, "MediaStore.insert")) { rc = 14; goto done; }
		if (!insertUri) { set_err(err, errlen, "камера недоступна: не удалось создать файл в MediaStore"); rc = 14; goto done; }
	}

	cIntent = (*env)->FindClass(env, "android/content/Intent");
	{
		jmethodID ctor = (*env)->GetMethodID(env, cIntent, "<init>", "(Ljava/lang/String;)V");
		jAction = (*env)->NewStringUTF(env, "android.media.action.IMAGE_CAPTURE");
		intent = (*env)->NewObject(env, cIntent, ctor, jAction);
		jmethodID putExtra = (*env)->GetMethodID(env, cIntent, "putExtra",
			"(Ljava/lang/String;Landroid/os/Parcelable;)Landroid/content/Intent;");
		jKey = (*env)->NewStringUTF(env, "output");
		(*env)->CallObjectMethod(env, intent, putExtra, jKey, insertUri);
		if (take_exception(env, err, errlen, "Intent.putExtra")) { rc = 15; goto done; }

		jmethodID addFlags = (*env)->GetMethodID(env, cIntent, "addFlags", "(I)Landroid/content/Intent;");
		// FLAG_ACTIVITY_NEW_TASK | FLAG_GRANT_WRITE_URI_PERMISSION | FLAG_GRANT_READ_URI_PERMISSION
		(*env)->CallObjectMethod(env, intent, addFlags, (jint)(0x10000000 | 0x00000001 | 0x00000002));
		if (take_exception(env, err, errlen, "Intent.addFlags")) { rc = 15; goto done; }

		jmethodID startAct = (*env)->GetMethodID(env, cCtx, "startActivity", "(Landroid/content/Intent;)V");
		(*env)->CallVoidMethod(env, app, startAct, intent);
		if (take_exception(env, err, errlen, "startActivity")) { rc = 16; goto done; }
	}

done:
	if (jName) (*env)->DeleteLocalRef(env, jName);
	if (jRel) (*env)->DeleteLocalRef(env, jRel);
	if (jAction) (*env)->DeleteLocalRef(env, jAction);
	if (jKey) (*env)->DeleteLocalRef(env, jKey);
	if (intent) (*env)->DeleteLocalRef(env, intent);
	if (insertUri) (*env)->DeleteLocalRef(env, insertUri);
	if (values) (*env)->DeleteLocalRef(env, values);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (cIntent) (*env)->DeleteLocalRef(env, cIntent);
	if (cMS) (*env)->DeleteLocalRef(env, cMS);
	if (cCV) (*env)->DeleteLocalRef(env, cCV);
	if (cCtx) (*env)->DeleteLocalRef(env, cCtx);
	if (cAT) (*env)->DeleteLocalRef(env, cAT);
	if (rc != 0 && err && errlen > 0 && err[0] == '\0') {
		set_err(err, errlen, "камера недоступна");
	}
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Capture запускает системную камеру. Снимок сохраняется в общую память по пути
// Относительно /storage/emulated/0: relativePath — папка, displayName — имя файла.
func Capture(displayName, relativePath string) error {
	cName := C.CString(displayName)
	defer C.free(unsafe.Pointer(cName))
	cRel := C.CString(relativePath)
	defer C.free(unsafe.Pointer(cRel))

	buf := make([]byte, 512)
	code := C.start_capture(cName, cRel, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
		if msg == "" {
			msg = "не удалось открыть камеру"
		}
		return errors.New(msg)
	}
	return nil
}
