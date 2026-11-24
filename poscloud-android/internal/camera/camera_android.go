//go:build android

// Package camera открывает системную камеру и забирает снимок.
//
// Java-классов в проекте нет, поэтому всё сделано через JNI: получаем JVM,
// берём контекст приложения, создаём через MediaStore запись для будущего
// снимка (content://-URI), отдаём её камере как EXTRA_OUTPUT и запускаем
// ACTION_IMAGE_CAPTURE. Камера пишет файл сама; читать его по обычному пути
// нельзя (файл принадлежит приложению камеры), поэтому приложение открывает
// собственную запись MediaStore и копирует содержимое к себе.
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

// get_env поднимает JVM и текущий поток.
static JavaVM* get_vm(char* err, int len) {
	void* lib = dlopen("libnativehelper.so", RTLD_NOW);
	if (!lib) {
		lib = dlopen("libart.so", RTLD_NOW);
	}
	if (!lib) {
		set_err(err, len, "нет доступа к JVM (libnativehelper)");
		return NULL;
	}
	jni_get_vms_fn get_vms = (jni_get_vms_fn)dlsym(lib, "JNI_GetCreatedJavaVMs");
	if (!get_vms) {
		set_err(err, len, "JNI_GetCreatedJavaVMs не найден");
		return NULL;
	}
	JavaVM* vm = NULL;
	jsize count = 0;
	if (get_vms(&vm, 1, &count) != JNI_OK || count < 1 || vm == NULL) {
		set_err(err, len, "JVM не запущена");
		return NULL;
	}
	return vm;
}

// app_context возвращает Application (он же Context).
static jobject app_context(JNIEnv* env, char* err, int len) {
	jclass cAT = (*env)->FindClass(env, "android/app/ActivityThread");
	if (take_exception(env, err, len, "ActivityThread")) {
		return NULL;
	}
	jmethodID current = (*env)->GetStaticMethodID(env, cAT, "currentApplication", "()Landroid/app/Application;");
	jobject app = (*env)->CallStaticObjectMethod(env, cAT, current);
	if (take_exception(env, err, len, "currentApplication")) {
		return NULL;
	}
	if (cAT) {
		(*env)->DeleteLocalRef(env, cAT);
	}
	if (!app) {
		set_err(err, len, "нет контекста приложения");
	}
	return app;
}

// resolver_of возвращает ContentResolver приложения.
static jobject resolver_of(JNIEnv* env, jobject app, char* err, int len) {
	jclass cCtx = (*env)->FindClass(env, "android/content/Context");
	jmethodID getResolver = (*env)->GetMethodID(env, cCtx, "getContentResolver", "()Landroid/content/ContentResolver;");
	jobject resolver = (*env)->CallObjectMethod(env, app, getResolver);
	if (take_exception(env, err, len, "getContentResolver")) {
		return NULL;
	}
	if (cCtx) {
		(*env)->DeleteLocalRef(env, cCtx);
	}
	return resolver;
}

// parse_uri разбирает строку content:// в объект Uri.
static jobject parse_uri(JNIEnv* env, const char* s, char* err, int len) {
	jclass cUri = (*env)->FindClass(env, "android/net/Uri");
	jmethodID parse = (*env)->GetStaticMethodID(env, cUri, "parse", "(Ljava/lang/String;)Landroid/net/Uri;");
	jstring jStr = (*env)->NewStringUTF(env, s);
	jobject uri = (*env)->CallStaticObjectMethod(env, cUri, parse, jStr);
	if (take_exception(env, err, len, "Uri.parse")) {
		return NULL;
	}
	if (jStr) {
		(*env)->DeleteLocalRef(env, jStr);
	}
	if (cUri) {
		(*env)->DeleteLocalRef(env, cUri);
	}
	return uri;
}

// open_path открывает файл системным приложением. Обычный file:// на Android 7+
// приводит к FileUriExposedException, поэтому берём content://-URI из MediaStore:
// файл должен быть системе известен (в общей памяти — да, в служебной папке
// приложения записи MediaStore нет, и открыть его чужому приложению нельзя).
static int open_path(const char* path, const char* mime, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return 1;
		}
		attached = 1;
	}
	int rc = 0;
	jlong id = -1;
	jobject app = NULL, resolver = NULL, cursor = NULL, collection = NULL, uri = NULL, intent = NULL;
	jclass cMS = NULL, cRes = NULL, cCursor = NULL, cStr = NULL, cIntent = NULL, cCtx = NULL;

	(*env)->EnsureLocalCapacity(env, 48);

	app = app_context(env, err, errlen);
	if (!app) { rc = 2; goto done; }
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) { rc = 3; goto done; }

	cMS = (*env)->FindClass(env, "android/provider/MediaStore$Files");
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	cCursor = (*env)->FindClass(env, "android/database/Cursor");
	cStr = (*env)->FindClass(env, "java/lang/String");
	if (!cMS || !cRes || !cCursor || !cStr) {
		set_err(err, errlen, "классы MediaStore не найдены");
		rc = 4;
		goto done;
	}

	{
		jmethodID getCU = (*env)->GetStaticMethodID(env, cMS, "getContentUri", "(Ljava/lang/String;)Landroid/net/Uri;");
		jstring ext = (*env)->NewStringUTF(env, "external");
		collection = (*env)->CallStaticObjectMethod(env, cMS, getCU, ext);
		(*env)->DeleteLocalRef(env, ext);
		if (take_exception(env, err, errlen, "MediaStore.getContentUri")) { rc = 5; goto done; }
	}

	{
		jobjectArray proj = (*env)->NewObjectArray(env, 1, cStr, NULL);
		(*env)->SetObjectArrayElement(env, proj, 0, (*env)->NewStringUTF(env, "_id"));
		jobjectArray args = (*env)->NewObjectArray(env, 1, cStr, NULL);
		(*env)->SetObjectArrayElement(env, args, 0, (*env)->NewStringUTF(env, path));
		jstring sel = (*env)->NewStringUTF(env, "_data = ?");
		jmethodID query = (*env)->GetMethodID(env, cRes, "query",
			"(Landroid/net/Uri;[Ljava/lang/String;Ljava/lang/String;[Ljava/lang/String;Ljava/lang/String;)Landroid/database/Cursor;");
		cursor = (*env)->CallObjectMethod(env, resolver, query, collection, proj, sel, args, NULL);
		(*env)->DeleteLocalRef(env, proj);
		(*env)->DeleteLocalRef(env, args);
		(*env)->DeleteLocalRef(env, sel);
		if (take_exception(env, err, errlen, "MediaStore.query")) { rc = 6; goto done; }
		if (!cursor) {
			set_err(err, errlen, "файл не найден в MediaStore");
			rc = 7;
			goto done;
		}
		jmethodID moveToFirst = (*env)->GetMethodID(env, cCursor, "moveToFirst", "()Z");
		jmethodID getLong = (*env)->GetMethodID(env, cCursor, "getLong", "(I)J");
		jmethodID closeCur = (*env)->GetMethodID(env, cCursor, "close", "()V");
		jboolean has = (*env)->CallBooleanMethod(env, cursor, moveToFirst);
		if (!take_exception(env, err, errlen, "Cursor.moveToFirst") && has) {
			id = (*env)->CallLongMethod(env, cursor, getLong, (jint)0);
		}
		(*env)->CallVoidMethod(env, cursor, closeCur);
		if (take_exception(env, err, errlen, "Cursor.close")) { rc = 8; goto done; }
		if (id < 0) {
			set_err(err, errlen, "файл не проиндексирован системой (служебная папка приложения или файл только что скопирован)");
			rc = 9;
			goto done;
		}
	}

	{
		jclass cCUr = (*env)->FindClass(env, "android/content/ContentUris");
		jmethodID withId = (*env)->GetStaticMethodID(env, cCUr, "withAppendedId", "(Landroid/net/Uri;J)Landroid/net/Uri;");
		uri = (*env)->CallStaticObjectMethod(env, cCUr, withId, collection, id);
		(*env)->DeleteLocalRef(env, cCUr);
		if (take_exception(env, err, errlen, "ContentUris.withAppendedId")) { rc = 10; goto done; }
	}

	cIntent = (*env)->FindClass(env, "android/content/Intent");
	cCtx = (*env)->FindClass(env, "android/content/Context");
	{
		jmethodID ctor = (*env)->GetMethodID(env, cIntent, "<init>", "(Ljava/lang/String;)V");
		jstring action = (*env)->NewStringUTF(env, "android.intent.action.VIEW");
		intent = (*env)->NewObject(env, cIntent, ctor, action);
		(*env)->DeleteLocalRef(env, action);
		jmethodID setDT = (*env)->GetMethodID(env, cIntent, "setDataAndType", "(Landroid/net/Uri;Ljava/lang/String;)Landroid/content/Intent;");
		jstring jMime = (*env)->NewStringUTF(env, mime);
		(*env)->CallObjectMethod(env, intent, setDT, uri, jMime);
		(*env)->DeleteLocalRef(env, jMime);
		if (take_exception(env, err, errlen, "Intent.setDataAndType")) { rc = 11; goto done; }
		jmethodID addFlags = (*env)->GetMethodID(env, cIntent, "addFlags", "(I)Landroid/content/Intent;");
		// FLAG_ACTIVITY_NEW_TASK | FLAG_GRANT_READ_URI_PERMISSION
		(*env)->CallObjectMethod(env, intent, addFlags, (jint)(0x10000000 | 0x00000001));
		if (take_exception(env, err, errlen, "Intent.addFlags")) { rc = 12; goto done; }
		jmethodID startAct = (*env)->GetMethodID(env, cCtx, "startActivity", "(Landroid/content/Intent;)V");
		(*env)->CallVoidMethod(env, app, startAct, intent);
		if (take_exception(env, err, errlen, "startActivity")) {
			set_err(err, errlen, "нет приложения, которое открывает такой файл");
			rc = 13;
			goto done;
		}
	}

done:
	if (cursor) (*env)->DeleteLocalRef(env, cursor);
	if (uri) (*env)->DeleteLocalRef(env, uri);
	if (collection) (*env)->DeleteLocalRef(env, collection);
	if (intent) (*env)->DeleteLocalRef(env, intent);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (cMS) (*env)->DeleteLocalRef(env, cMS);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (cCursor) (*env)->DeleteLocalRef(env, cCursor);
	if (cStr) (*env)->DeleteLocalRef(env, cStr);
	if (cIntent) (*env)->DeleteLocalRef(env, cIntent);
	if (cCtx) (*env)->DeleteLocalRef(env, cCtx);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// app_version читает versionName пакета из PackageManager: его проставляет
// build.sh в манифесте, и приложение знает свою версию без ldflags (у fyne
// package их нет).
static int app_version(const char* pkg, char* out, int outlen, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return 1;
		}
		attached = 1;
	}
	int rc = 0;
	jobject app = NULL, pm = NULL, info = NULL;
	jclass cCtx = NULL, cPM = NULL, cPI = NULL;
	jstring name = NULL;
	(*env)->EnsureLocalCapacity(env, 32);

	app = app_context(env, err, errlen);
	if (!app) { rc = 2; goto done; }
	cCtx = (*env)->FindClass(env, "android/content/Context");
	cPM = (*env)->FindClass(env, "android/content/pm/PackageManager");
	cPI = (*env)->FindClass(env, "android/content/pm/PackageInfo");
	if (!cCtx || !cPM || !cPI) {
		set_err(err, errlen, "классы PackageManager не найдены");
		rc = 3;
		goto done;
	}
	{
		jmethodID getPM = (*env)->GetMethodID(env, cCtx, "getPackageManager", "()Landroid/content/pm/PackageManager;");
		pm = (*env)->CallObjectMethod(env, app, getPM);
		if (take_exception(env, err, errlen, "getPackageManager")) { rc = 4; goto done; }
	}
	{
		jmethodID getInfo = (*env)->GetMethodID(env, cPM, "getPackageInfo",
			"(Ljava/lang/String;I)Landroid/content/pm/PackageInfo;");
		jstring jPkg = (*env)->NewStringUTF(env, pkg);
		info = (*env)->CallObjectMethod(env, pm, getInfo, jPkg, (jint)0);
		(*env)->DeleteLocalRef(env, jPkg);
		if (take_exception(env, err, errlen, "getPackageInfo")) { rc = 5; goto done; }
	}
	{
		jfieldID fVersion = (*env)->GetFieldID(env, cPI, "versionName", "Ljava/lang/String;");
		name = (jstring)(*env)->GetObjectField(env, info, fVersion);
		if (take_exception(env, err, errlen, "PackageInfo.versionName")) { rc = 6; goto done; }
		if (!name) {
			set_err(err, errlen, "в манифесте нет versionName");
			rc = 7;
			goto done;
		}
		const char* utf = (*env)->GetStringUTFChars(env, name, NULL);
		if (!utf) {
			set_err(err, errlen, "версия не прочитана");
			rc = 8;
			goto done;
		}
		snprintf(out, outlen, "%s", utf);
		(*env)->ReleaseStringUTFChars(env, name, utf);
	}

done:
	if (name) (*env)->DeleteLocalRef(env, name);
	if (info) (*env)->DeleteLocalRef(env, info);
	if (pm) (*env)->DeleteLocalRef(env, pm);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (cCtx) (*env)->DeleteLocalRef(env, cCtx);
	if (cPM) (*env)->DeleteLocalRef(env, cPM);
	if (cPI) (*env)->DeleteLocalRef(env, cPI);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// list_screenshots перечисляет снимки экрана из MediaStore:
// строки «id\tимя\tразмер\n» (не больше 200 штук).
static int list_screenshots(char* out, int out_len, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return 2;
		}
		attached = 1;
	}
	int rc = 0;
	if (out && out_len > 0) {
		out[0] = 0;
	}
	jobject app = NULL, resolver = NULL, cursor = NULL;
	jobjectArray proj = NULL, args = NULL;
	jclass cRes = NULL, cCursor = NULL, cStr = NULL, cMS = NULL;
	jstring sel = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		rc = 3;
		goto done;
	}
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) {
		rc = 4;
		goto done;
	}
	cMS = (*env)->FindClass(env, "android/provider/MediaStore$Images$Media");
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	cCursor = (*env)->FindClass(env, "android/database/Cursor");
	cStr = (*env)->FindClass(env, "java/lang/String");
	(*env)->EnsureLocalCapacity(env, 48);
	{
		jfieldID fUri = (*env)->GetStaticFieldID(env, cMS, "EXTERNAL_CONTENT_URI", "Landroid/net/Uri;");
		jobject collection = (*env)->GetStaticObjectField(env, cMS, fUri);

		// relative_path появился в Android 10 (API 29); на старых версиях путь
		// берём из _data. Отбираем только подпапки Screenshots, без чужих снимков.
		jint sdk = 0;
		jclass cVer = (*env)->FindClass(env, "android/os/Build$VERSION");
		if (cVer) {
			jfieldID fSdk = (*env)->GetStaticFieldID(env, cVer, "SDK_INT", "I");
			if (fSdk) {
				sdk = (*env)->GetStaticIntField(env, cVer, fSdk);
			}
			(*env)->DeleteLocalRef(env, cVer);
		}
		const char* path_col = sdk >= 29 ? "relative_path" : "_data";
		const char* a0 = sdk >= 29 ? "Pictures/Screenshots%" : "%Pictures/Screenshots/%";
		const char* a1 = sdk >= 29 ? "DCIM/Screenshots%" : "%DCIM/Screenshots/%";

		proj = (*env)->NewObjectArray(env, 4, cStr, NULL);
		(*env)->SetObjectArrayElement(env, proj, 0, (*env)->NewStringUTF(env, "_id"));
		(*env)->SetObjectArrayElement(env, proj, 1, (*env)->NewStringUTF(env, "_display_name"));
		(*env)->SetObjectArrayElement(env, proj, 2, (*env)->NewStringUTF(env, "_size"));
		(*env)->SetObjectArrayElement(env, proj, 3, (*env)->NewStringUTF(env, path_col));

		args = (*env)->NewObjectArray(env, 2, cStr, NULL);
		(*env)->SetObjectArrayElement(env, args, 0, (*env)->NewStringUTF(env, a0));
		(*env)->SetObjectArrayElement(env, args, 1, (*env)->NewStringUTF(env, a1));
		sel = (*env)->NewStringUTF(env, sdk >= 29 ? "relative_path LIKE ? OR relative_path LIKE ?"
			: "_data LIKE ? OR _data LIKE ?");

		jmethodID query = (*env)->GetMethodID(env, cRes, "query",
			"(Landroid/net/Uri;[Ljava/lang/String;Ljava/lang/String;[Ljava/lang/String;Ljava/lang/String;)Landroid/database/Cursor;");
		// Свежие снимки — первыми и не больше 200: иначе после двухсот снимков
		// новые никогда не попадают в выборку.
		jstring order = (*env)->NewStringUTF(env, "date_added DESC LIMIT 200");
		cursor = (*env)->CallObjectMethod(env, resolver, query, collection, proj, sel, args, order);
		(*env)->DeleteLocalRef(env, order);
		if (take_exception(env, err, errlen, "MediaStore.query")) {
			rc = 5;
			goto done;
		}
		if (!cursor) {
			rc = 0;
			goto done;
		}
	}

	{
		jmethodID move = (*env)->GetMethodID(env, cCursor, "moveToNext", "()Z");
		jmethodID getLong = (*env)->GetMethodID(env, cCursor, "getLong", "(I)J");
		jmethodID getString = (*env)->GetMethodID(env, cCursor, "getString", "(I)Ljava/lang/String;");
		int off = 0, rows = 0;
		while (rows < 200) {
			if (!(*env)->CallBooleanMethod(env, cursor, move)) {
				break;
			}
			if (take_exception(env, err, errlen, "Cursor.moveToNext")) {
				rc = 6;
				break;
			}
			jlong id = (*env)->CallLongMethod(env, cursor, getLong, 0);
			jstring name = (jstring)(*env)->CallObjectMethod(env, cursor, getString, 1);
			jlong size = (*env)->CallLongMethod(env, cursor, getLong, 2);
			const char* nm = name ? (*env)->GetStringUTFChars(env, name, NULL) : NULL;
			if (nm) {
				int room = out_len - off - 1;
				if (room < 64) {
					(*env)->ReleaseStringUTFChars(env, name, nm);
					break;
				}
				int w = snprintf(out + off, room, "%lld\t%s\t%lld\n", (long long)id, nm, (long long)size);
				if (w > 0) {
					off += w;
				}
				(*env)->ReleaseStringUTFChars(env, name, nm);
			}
			if (name) {
				(*env)->DeleteLocalRef(env, name);
			}
			rows++;
		}
		if (out && out_len > off) {
			out[off] = 0;
		}
	}

	done:
	// Cursor — нативный ресурс (CursorWindow): без close() он живёт до финализатора,
	// и при опросе каждые 30 с в logcat сыпется «A resource failed to call Cursor.close».
	if (cursor && cCursor) {
		jmethodID close = (*env)->GetMethodID(env, cCursor, "close", "()V");
		if (close) {
			(*env)->CallVoidMethod(env, cursor, close);
			if ((*env)->ExceptionCheck(env)) {
				(*env)->ExceptionClear(env);
			}
		}
	}
	if (cursor) (*env)->DeleteLocalRef(env, cursor);
	if (proj) (*env)->DeleteLocalRef(env, proj);
	if (args) (*env)->DeleteLocalRef(env, args);
	if (sel) (*env)->DeleteLocalRef(env, sel);
	if (cCursor) (*env)->DeleteLocalRef(env, cCursor);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (cStr) (*env)->DeleteLocalRef(env, cStr);
	if (cMS) (*env)->DeleteLocalRef(env, cMS);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// start_capture создаёт запись в MediaStore и запускает камеру.
static int start_capture(const char* display_name, const char* relative_path, char* out_uri, int out_uri_len, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			set_err(err, errlen, "не удалось подключиться к JVM");
			return 2;
		}
		attached = 1;
	}

	int rc = 0;
	jobject app = NULL, resolver = NULL, values = NULL, insertUri = NULL, intent = NULL;
	jclass cCV = NULL, cMS = NULL, cIntent = NULL, cCtx = NULL, cRes = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		rc = 3;
		goto done;
	}
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) {
		rc = 4;
		goto done;
	}

	cCV = (*env)->FindClass(env, "android/content/ContentValues");
	{
		jmethodID ctor = (*env)->GetMethodID(env, cCV, "<init>", "()V");
		jmethodID put = (*env)->GetMethodID(env, cCV, "put", "(Ljava/lang/String;Ljava/lang/String;)V");
		values = (*env)->NewObject(env, cCV, ctor);
		jstring kName = (*env)->NewStringUTF(env, "_display_name");
		jstring kMime = (*env)->NewStringUTF(env, "mime_type");
		jstring kRel = (*env)->NewStringUTF(env, "relative_path");
		jstring vName = (*env)->NewStringUTF(env, display_name);
		jstring vMime = (*env)->NewStringUTF(env, "image/jpeg");
		jstring vRel = (*env)->NewStringUTF(env, relative_path);
		(*env)->CallVoidMethod(env, values, put, kName, vName);
		(*env)->CallVoidMethod(env, values, put, kMime, vMime);
		(*env)->CallVoidMethod(env, values, put, kRel, vRel);
		if (take_exception(env, err, errlen, "ContentValues")) {
			rc = 5;
			goto done;
		}
	}

	cMS = (*env)->FindClass(env, "android/provider/MediaStore$Images$Media");
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	{
		jfieldID fUri = (*env)->GetStaticFieldID(env, cMS, "EXTERNAL_CONTENT_URI", "Landroid/net/Uri;");
		jobject collection = (*env)->GetStaticObjectField(env, cMS, fUri);
		jmethodID insert = (*env)->GetMethodID(env, cRes, "insert",
			"(Landroid/net/Uri;Landroid/content/ContentValues;)Landroid/net/Uri;");
		insertUri = (*env)->CallObjectMethod(env, resolver, insert, collection, values);
		if (take_exception(env, err, errlen, "MediaStore.insert")) {
			rc = 6;
			goto done;
		}
		if (!insertUri) {
			set_err(err, errlen, "не удалось создать запись в MediaStore");
			rc = 6;
			goto done;
		}
		if (out_uri && out_uri_len > 0) {
			jclass cUri = (*env)->FindClass(env, "android/net/Uri");
			jmethodID toString = (*env)->GetMethodID(env, cUri, "toString", "()Ljava/lang/String;");
			jstring s = (jstring)(*env)->CallObjectMethod(env, insertUri, toString);
			if (s) {
				const char* utf = (*env)->GetStringUTFChars(env, s, NULL);
				if (utf) {
					snprintf(out_uri, out_uri_len, "%s", utf);
					(*env)->ReleaseStringUTFChars(env, s, utf);
				}
				(*env)->DeleteLocalRef(env, s);
			}
			if (cUri) {
				(*env)->DeleteLocalRef(env, cUri);
			}
		}
	}

	cCtx = (*env)->FindClass(env, "android/content/Context");
	cIntent = (*env)->FindClass(env, "android/content/Intent");
	{
		jmethodID ctor = (*env)->GetMethodID(env, cIntent, "<init>", "(Ljava/lang/String;)V");
		jstring action = (*env)->NewStringUTF(env, "android.media.action.IMAGE_CAPTURE");
		intent = (*env)->NewObject(env, cIntent, ctor, action);
		jmethodID putExtra = (*env)->GetMethodID(env, cIntent, "putExtra",
			"(Ljava/lang/String;Landroid/os/Parcelable;)Landroid/content/Intent;");
		jstring key = (*env)->NewStringUTF(env, "output");
		(*env)->CallObjectMethod(env, intent, putExtra, key, insertUri);
		if (take_exception(env, err, errlen, "Intent.putExtra")) {
			rc = 7;
			goto done;
		}
		jmethodID addFlags = (*env)->GetMethodID(env, cIntent, "addFlags", "(I)Landroid/content/Intent;");
		// FLAG_ACTIVITY_NEW_TASK | FLAG_GRANT_WRITE_URI_PERMISSION | FLAG_GRANT_READ_URI_PERMISSION
		(*env)->CallObjectMethod(env, intent, addFlags, (jint)(0x10000000 | 0x00000001 | 0x00000002));
		if (take_exception(env, err, errlen, "Intent.addFlags")) {
			rc = 7;
			goto done;
		}
		jmethodID startAct = (*env)->GetMethodID(env, cCtx, "startActivity", "(Landroid/content/Intent;)V");
		(*env)->CallVoidMethod(env, app, startAct, intent);
		if (take_exception(env, err, errlen, "startActivity")) {
			rc = 8;
			goto done;
		}
	}

done:
	if (intent) (*env)->DeleteLocalRef(env, intent);
	if (insertUri) (*env)->DeleteLocalRef(env, insertUri);
	if (values) (*env)->DeleteLocalRef(env, values);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (cIntent) (*env)->DeleteLocalRef(env, cIntent);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (cMS) (*env)->DeleteLocalRef(env, cMS);
	if (cCV) (*env)->DeleteLocalRef(env, cCV);
	if (cCtx) (*env)->DeleteLocalRef(env, cCtx);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// uri_size возвращает размер снимка по записи MediaStore (-1 — ещё нет данных).
static jlong uri_size(const char* uri_str, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return -1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return -1;
		}
		attached = 1;
	}
	jlong size = -1;
	jobject app = NULL, resolver = NULL, uri = NULL, pfd = NULL;
	jclass cRes = NULL, cPfd = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		goto done;
	}
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) {
		goto done;
	}
	uri = parse_uri(env, uri_str, err, errlen);
	if (!uri) {
		goto done;
	}
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	cPfd = (*env)->FindClass(env, "android/os/ParcelFileDescriptor");
	{
		jmethodID openFd = (*env)->GetMethodID(env, cRes, "openFileDescriptor",
			"(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");
		jstring mode = (*env)->NewStringUTF(env, "r");
		pfd = (*env)->CallObjectMethod(env, resolver, openFd, uri, mode);
		if (take_exception(env, err, errlen, "openFileDescriptor")) {
			goto done;
		}
		if (pfd) {
			jmethodID getStat = (*env)->GetMethodID(env, cPfd, "getStatSize", "()J");
			size = (*env)->CallLongMethod(env, pfd, getStat);
			jmethodID close = (*env)->GetMethodID(env, cPfd, "close", "()V");
			(*env)->CallVoidMethod(env, pfd, close);
		}
	}

done:
	if (pfd) (*env)->DeleteLocalRef(env, pfd);
	if (uri) (*env)->DeleteLocalRef(env, uri);
	if (cPfd) (*env)->DeleteLocalRef(env, cPfd);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return size;
}

// open_read_fd отдаёт файловый дескриптор снимка (-1 при ошибке).
static int open_read_fd(const char* uri_str, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return -1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return -1;
		}
		attached = 1;
	}
	int fd = -1;
	jobject app = NULL, resolver = NULL, uri = NULL, pfd = NULL;
	jclass cRes = NULL, cPfd = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		goto done;
	}
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) {
		goto done;
	}
	uri = parse_uri(env, uri_str, err, errlen);
	if (!uri) {
		goto done;
	}
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	cPfd = (*env)->FindClass(env, "android/os/ParcelFileDescriptor");
	{
		jmethodID openFd = (*env)->GetMethodID(env, cRes, "openFileDescriptor",
			"(Landroid/net/Uri;Ljava/lang/String;)Landroid/os/ParcelFileDescriptor;");
		jstring mode = (*env)->NewStringUTF(env, "r");
		pfd = (*env)->CallObjectMethod(env, resolver, openFd, uri, mode);
		if (take_exception(env, err, errlen, "openFileDescriptor")) {
			goto done;
		}
		if (pfd) {
			jmethodID detach = (*env)->GetMethodID(env, cPfd, "detachFd", "()I");
			fd = (int)(*env)->CallIntMethod(env, pfd, detach);
			if (take_exception(env, err, errlen, "detachFd")) {
				fd = -1;
			}
		}
	}

done:
	if (pfd) (*env)->DeleteLocalRef(env, pfd);
	if (uri) (*env)->DeleteLocalRef(env, uri);
	if (cPfd) (*env)->DeleteLocalRef(env, cPfd);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return fd;
}

// delete_uri удаляет запись MediaStore вместе с файлом.
static int delete_uri(const char* uri_str, char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return 2;
		}
		attached = 1;
	}
	int rc = 0;
	jobject app = NULL, resolver = NULL, uri = NULL;
	jclass cRes = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		rc = 3;
		goto done;
	}
	resolver = resolver_of(env, app, err, errlen);
	if (!resolver) {
		rc = 4;
		goto done;
	}
	uri = parse_uri(env, uri_str, err, errlen);
	if (!uri) {
		rc = 5;
		goto done;
	}
	cRes = (*env)->FindClass(env, "android/content/ContentResolver");
	{
		jmethodID del = (*env)->GetMethodID(env, cRes, "delete",
			"(Landroid/net/Uri;Ljava/lang/String;[Ljava/lang/String;)I");
		(*env)->CallIntMethod(env, resolver, del, uri, NULL, NULL);
		if (take_exception(env, err, errlen, "MediaStore.delete")) {
			rc = 6;
		}
	}

done:
	if (uri) (*env)->DeleteLocalRef(env, uri);
	if (cRes) (*env)->DeleteLocalRef(env, cRes);
	if (resolver) (*env)->DeleteLocalRef(env, resolver);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// sdk_int возвращает Build.VERSION.SDK_INT (0 — не удалось прочитать).
static int sdk_int(JNIEnv* env) {
	jclass cVer = (*env)->FindClass(env, "android/os/Build$VERSION");
	if (!cVer) {
		(*env)->ExceptionClear(env);
		return 0;
	}
	jfieldID f = (*env)->GetStaticFieldID(env, cVer, "SDK_INT", "I");
	int v = 0;
	if (f) {
		v = (int)(*env)->GetStaticIntField(env, cVer, f);
	}
	(*env)->ExceptionClear(env);
	(*env)->DeleteLocalRef(env, cVer);
	return v;
}

// current_activity возвращает GoNativeActivity из статического поля класса
// (конструктор Activity сохраняет туда себя). NULL — окно ещё не создано.
// Это собственный класс приложения, а не скрытый API системы.
static jobject current_activity(JNIEnv* env, char* err, int len) {
	jclass cCls = (*env)->FindClass(env, "org/golang/app/GoNativeActivity");
	if (take_exception(env, err, len, "FindClass GoNativeActivity")) {
		return NULL;
	}
	jfieldID f = (*env)->GetStaticFieldID(env, cCls, "goNativeActivity", "Lorg/golang/app/GoNativeActivity;");
	if (take_exception(env, err, len, "GetStaticFieldID goNativeActivity")) {
		(*env)->DeleteLocalRef(env, cCls);
		return NULL;
	}
	jobject act = (*env)->GetStaticObjectField(env, cCls, f);
	(*env)->DeleteLocalRef(env, cCls);
	if (!act) {
		set_err(err, len, "окно приложения ещё не создано");
	}
	return act;
}

// check_permission: 1 — разрешение выдано, 0 — нет, -1 — ошибка.
// До Android 6 (API 23) разрешения выдаются при установке — считаем выданным.
static int check_permission(JNIEnv* env, const char* perm, char* err, int errlen) {
	if (sdk_int(env) < 23) {
		return 1;
	}
	int rc = -1;
	jobject app = app_context(env, err, errlen);
	if (!app) {
		return -1;
	}
	jclass cCtx = (*env)->FindClass(env, "android/content/Context");
	jmethodID check = (*env)->GetMethodID(env, cCtx, "checkSelfPermission", "(Ljava/lang/String;)I");
	jstring p = (*env)->NewStringUTF(env, perm);
	jint r = (*env)->CallIntMethod(env, app, check, p);
	if (take_exception(env, err, errlen, "checkSelfPermission")) {
		rc = -1;
	} else {
		rc = (r == 0) ? 1 : 0;  // PERMISSION_GRANTED = 0
	}
	(*env)->DeleteLocalRef(env, p);
	(*env)->DeleteLocalRef(env, cCtx);
	(*env)->DeleteLocalRef(env, app);
	return rc;
}

// photo_permission проверяет оба разрешения на чтение фото: READ_MEDIA_IMAGES
// (Android 13+) и READ_EXTERNAL_STORAGE (старые версии и приложения с targetSdk < 33).
static int photo_permission(char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return -1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return -1;
		}
		attached = 1;
	}
	int rc = check_permission(env, "android.permission.READ_MEDIA_IMAGES", err, errlen);
	if (rc != 0) {
		goto done_perm;  // выдано или ошибка
	}
	rc = check_permission(env, "android.permission.READ_EXTERNAL_STORAGE", err, errlen);
	if (rc == 0) {
		set_err(err, errlen, "");
	}
done_perm:
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// request_photo_permission показывает системный диалог разрешения на чтение
// фото. Без него MediaStore молча отдаёт только собственные файлы приложения.
static int request_photo_permission(char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			set_err(err, errlen, "не удалось подключиться к JVM");
			return 2;
		}
		attached = 1;
	}
	int rc = 0;
	jobject act = NULL;
	jobjectArray arr = NULL;
	jclass cAct = NULL, cStr = NULL;

	act = current_activity(env, err, errlen);
	if (!act) {
		rc = 3;
		goto done_req;
	}
	cAct = (*env)->FindClass(env, "android/app/Activity");
	cStr = (*env)->FindClass(env, "java/lang/String");
	jmethodID req = (*env)->GetMethodID(env, cAct, "requestPermissions", "([Ljava/lang/String;I)V");
	if (take_exception(env, err, errlen, "requestPermissions")) {
		rc = 4;
		goto done_req;
	}
	// Необъявленное в манифесте разрешение система просто отклоняет —
	// поэтому просим оба: какой объявлен, тот и покажется в диалоге.
	arr = (*env)->NewObjectArray(env, 2, cStr, NULL);
	(*env)->SetObjectArrayElement(env, arr, 0, (*env)->NewStringUTF(env, "android.permission.READ_MEDIA_IMAGES"));
	(*env)->SetObjectArrayElement(env, arr, 1, (*env)->NewStringUTF(env, "android.permission.READ_EXTERNAL_STORAGE"));
	(*env)->CallVoidMethod(env, act, req, arr, (jint)7301);
	if (take_exception(env, err, errlen, "requestPermissions")) {
		rc = 5;
	}

done_req:
	if (arr) (*env)->DeleteLocalRef(env, arr);
	if (cAct) (*env)->DeleteLocalRef(env, cAct);
	if (cStr) (*env)->DeleteLocalRef(env, cStr);
	if (act) (*env)->DeleteLocalRef(env, act);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}

// open_app_settings открывает страницу приложения в настройках системы:
// разрешение, отклонённое дважды, система больше не спрашивает — включить
// его можно только там.
static int open_app_settings(char* err, int errlen) {
	JavaVM* vm = get_vm(err, errlen);
	if (!vm) {
		return 1;
	}
	JNIEnv* env = NULL;
	int attached = 0;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) {
			return 2;
		}
		attached = 1;
	}
	int rc = 0;
	jobject app = NULL, pkg = NULL, uri = NULL, intent = NULL;
	jclass cCtx = NULL, cIntent = NULL;

	app = app_context(env, err, errlen);
	if (!app) {
		rc = 3;
		goto done_set;
	}
	cCtx = (*env)->FindClass(env, "android/content/Context");
	cIntent = (*env)->FindClass(env, "android/content/Intent");
	{
		jmethodID getPkg = (*env)->GetMethodID(env, cCtx, "getPackageName", "()Ljava/lang/String;");
		pkg = (*env)->CallObjectMethod(env, app, getPkg);
		if (take_exception(env, err, errlen, "getPackageName")) {
			rc = 4;
			goto done_set;
		}
		const char* pkgUtf = pkg ? (*env)->GetStringUTFChars(env, pkg, NULL) : NULL;
		char uriStr[300];
		snprintf(uriStr, sizeof uriStr, "package:%s", pkgUtf ? pkgUtf : "");
		if (pkgUtf) {
			(*env)->ReleaseStringUTFChars(env, pkg, pkgUtf);
		}
		uri = parse_uri(env, uriStr, err, errlen);
		if (!uri) {
			rc = 5;
			goto done_set;
		}
		jmethodID ctor = (*env)->GetMethodID(env, cIntent, "<init>", "(Ljava/lang/String;)V");
		jstring action = (*env)->NewStringUTF(env, "android.settings.APPLICATION_DETAILS_SETTINGS");
		intent = (*env)->NewObject(env, cIntent, ctor, action);
		(*env)->DeleteLocalRef(env, action);
		jmethodID setData = (*env)->GetMethodID(env, cIntent, "setData", "(Landroid/net/Uri;)Landroid/content/Intent;");
		(*env)->CallObjectMethod(env, intent, setData, uri);
		jmethodID addFlags = (*env)->GetMethodID(env, cIntent, "addFlags", "(I)Landroid/content/Intent;");
		(*env)->CallObjectMethod(env, intent, addFlags, (jint)0x10000000);  // FLAG_ACTIVITY_NEW_TASK
		jmethodID startAct = (*env)->GetMethodID(env, cCtx, "startActivity", "(Landroid/content/Intent;)V");
		(*env)->CallVoidMethod(env, app, startAct, intent);
		if (take_exception(env, err, errlen, "startActivity settings")) {
			rc = 6;
		}
	}

done_set:
	if (intent) (*env)->DeleteLocalRef(env, intent);
	if (uri) (*env)->DeleteLocalRef(env, uri);
	if (pkg) (*env)->DeleteLocalRef(env, pkg);
	if (cIntent) (*env)->DeleteLocalRef(env, cIntent);
	if (cCtx) (*env)->DeleteLocalRef(env, cCtx);
	if (app) (*env)->DeleteLocalRef(env, app);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
	return rc;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

// Shot — снимок экрана из MediaStore.
type Shot struct {
	ID   int64
	Name string
	Size int64
	URI  string
}

// Screenshots перечисляет снимки экрана из MediaStore: только так можно
// получить чужие файлы (системный скриншот принадлежит SystemUI, и чтение
// по обычному пути даёт permission denied).
func Screenshots() ([]Shot, error) {
	buf := make([]byte, 64<<10)
	errBuf := make([]byte, 256)
	code := C.list_screenshots(
		(*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось получить список снимков экрана"
		}
		return nil, errors.New(msg)
	}
	text := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
	var out []Shot
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		id, errID := strconv.ParseInt(parts[0], 10, 64)
		size, errSize := strconv.ParseInt(parts[2], 10, 64)
		if errID != nil || errSize != nil {
			continue
		}
		out = append(out, Shot{
			ID:   id,
			Name: parts[1],
			Size: size,
			URI:  fmt.Sprintf("content://media/external/images/media/%d", id),
		})
	}
	return out, nil
}

// Capture запускает системную камеру. Снимок создаётся в общей памяти
// (relativePath — папка относительно /storage/emulated/0, например Pictures/POScloud),
// displayName — имя файла. Возвращает content://-URI созданной записи.
func Capture(displayName, relativePath string) (string, error) {
	cName := C.CString(displayName)
	defer C.free(unsafe.Pointer(cName))
	cRel := C.CString(relativePath)
	defer C.free(unsafe.Pointer(cRel))

	errBuf := make([]byte, 512)
	uriBuf := make([]byte, 512)
	code := C.start_capture(cName, cRel,
		(*C.char)(unsafe.Pointer(&uriBuf[0])), C.int(len(uriBuf)),
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось открыть камеру"
		}
		return "", errors.New(msg)
	}
	return C.GoString((*C.char)(unsafe.Pointer(&uriBuf[0]))), nil
}

// Size возвращает размер готового снимка; -1 — данных ещё нет.
func Size(uri string) int64 {
	cURI := C.CString(uri)
	defer C.free(unsafe.Pointer(cURI))
	errBuf := make([]byte, 256)
	return int64(C.uri_size(cURI, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))))
}

// Open отдаёт файл снимка на чтение (по записи MediaStore, а не по пути:
// файл принадлежит приложению камеры, и обычное открытие даёт permission denied).
func Open(uri string) (*os.File, error) {
	cURI := C.CString(uri)
	defer C.free(unsafe.Pointer(cURI))
	errBuf := make([]byte, 256)
	fd := C.open_read_fd(cURI, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if fd < 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось открыть снимок"
		}
		return nil, errors.New(msg)
	}
	return os.NewFile(uintptr(fd), "poscloud-photo"), nil
}

// Delete убирает запись MediaStore вместе с исходным файлом.
func Delete(uri string) error {
	if uri == "" {
		return nil
	}
	cURI := C.CString(uri)
	defer C.free(unsafe.Pointer(cURI))
	errBuf := make([]byte, 256)
	if code := C.delete_uri(cURI, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))); code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось удалить запись MediaStore"
		}
		return errors.New(msg)
	}
	return nil
}

// ---------- разрешение на чтение фото ----------
//
// Снимки экрана создаёт системное приложение. Пока пользователем не выдано
// разрешение на чтение фото, MediaStore на запрос отвечает пустым списком
// (приложение видит только собственные файлы) — без ошибки.

// PhotoPermissionGranted сообщает, выдано ли разрешение читать фото
// других приложений.
func PhotoPermissionGranted() bool {
	errBuf := make([]byte, 256)
	return int(C.photo_permission((*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))) == 1
}

// RequestPhotoPermission показывает системный диалог разрешения на чтение
// фото. Результат приходит в Activity, поэтому после ответа пользователя
// разрешение проверяется повторно (PhotoPermissionGranted).
func RequestPhotoPermission() error {
	errBuf := make([]byte, 512)
	if code := C.request_photo_permission((*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))); code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось запросить разрешение"
		}
		return errors.New(msg)
	}
	return nil
}

// OpenAppSettings открывает страницу приложения в настройках системы.
func OpenAppSettings() error {
	errBuf := make([]byte, 512)
	if code := C.open_app_settings((*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf))); code != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		if msg == "" {
			msg = "не удалось открыть настройки"
		}
		return errors.New(msg)
	}
	return nil
}

// MimeFor — MIME-тип по расширению файла: он нужен MediaStore и Intent, чтобы
// система выбрала приложение для просмотра.
func MimeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".heic":
		return "image/heic"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".log", ".md", ".json", ".xml", ".csv":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi", ".divx":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/x-wav"
	case ".zip":
		return "application/zip"
	case ".apk":
		return "application/vnd.android.package-archive"
	}
	return ""
}

// OpenFile открывает файл системным приложением: картинку — просмотрщиком,
// APK — установщиком, PDF — читалкой. Работает для файлов, известных MediaStore
// (такие лежат в общей памяти); файл из служебной папки приложения система
// другому приложению не отдаст — об этом честно сообщаем.
func OpenFile(path string) error {
	mime := MimeFor(path)
	if mime == "" {
		return fmt.Errorf("не знаю, чем открывать «%s»", filepath.Base(path))
	}
	errBuf := make([]byte, 256)
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	cMime := C.CString(mime)
	defer C.free(unsafe.Pointer(cMime))
	code := C.open_path(cPath, cMime, (*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if code != 0 {
		msg := strings.TrimSpace(C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
		if msg == "" {
			msg = "не удалось открыть файл"
		}
		return errors.New(msg)
	}
	return nil
}

// AppVersion — версия приложения из манифеста (пустая, если прочитать не удалось).
func AppVersion(pkg string) string {
	buf := make([]byte, 64)
	errBuf := make([]byte, 256)
	cPkg := C.CString(pkg)
	defer C.free(unsafe.Pointer(cPkg))
	code := C.app_version(cPkg,
		(*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if code != 0 {
		return ""
	}
	return strings.TrimSpace(C.GoString((*C.char)(unsafe.Pointer(&buf[0]))))
}