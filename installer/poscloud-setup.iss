; POSCloud — установщик с выбором компонентов «сервер» и/или «клиент».
; Компиляция: ISCC.exe poscloud-setup.iss   (или .\build.ps1 из этой папки)

#define MyAppName "POSCloud"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "reznikov73"
#define MyAppURL "https://github.com/reznikov73/POSCloud"
#define RepoRoot ".."

[Setup]
AppId={{A308A16C-0226-408F-B681-41C50652DCE2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
VersionInfoVersion=1.0.0.0
VersionInfoCompany={#MyAppPublisher}
VersionInfoProductName={#MyAppName}
DefaultDirName={autopf}\POSCloud
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir=out
OutputBaseFilename=POSCloud-Setup-{#MyAppVersion}
SetupIconFile={#RepoRoot}\poscloud-server\build\windows\icon.ico
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
CloseApplications=yes
RestartApplications=no
UninstallDisplayName={#MyAppName} {#MyAppVersion}

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Components]
Name: "server"; Description: "Сервер POSCloud — хранит файлы пользователей и отдаёт их клиентам (для серверного ПК)"; Types: full
Name: "client"; Description: "Клиент POSCloud — синхронизирует выбранную папку с сервером (для рабочих ПК)"; Types: full

[Files]
Source: "{#RepoRoot}\poscloud-server\build\bin\poscloud-server.exe"; DestDir: "{app}"; Components: server; Flags: ignoreversion
Source: "{#RepoRoot}\poscloud-client\build\bin\poscloud-client.exe"; DestDir: "{app}"; Components: client; Flags: ignoreversion

[Icons]
Name: "{group}\POSCloud Server"; Filename: "{app}\poscloud-server.exe"; Components: server
Name: "{group}\POSCloud Client"; Filename: "{app}\poscloud-client.exe"; Components: client
Name: "{group}\{cm:UninstallProgram,POSCloud}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\POSCloud Server"; Filename: "{app}\poscloud-server.exe"; Components: server; Tasks: desktopicon
Name: "{autodesktop}\POSCloud Client"; Filename: "{app}\poscloud-client.exe"; Components: client; Tasks: desktopicon

[Run]
Filename: "{app}\poscloud-server.exe"; Description: "Запустить POSCloud Server"; Flags: nowait postinstall skipifsilent; Components: server
Filename: "{app}\poscloud-client.exe"; Description: "Запустить POSCloud Client"; Flags: nowait postinstall skipifsilent; Components: client

[Code]
function WebView2Installed: Boolean;
begin
  Result :=
    RegKeyExists(HKLM, 'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}') or
    RegKeyExists(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}') or
    RegKeyExists(HKCU, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}');
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpReady) and (not WebView2Installed) then
    MsgBox('Не найден компонент Microsoft Edge WebView2 Runtime, нужный для работы приложений POSCloud.' + #13#10 +
           'На Windows 11 он уже встроен; для Windows 10 установите его с сайта Microsoft:' + #13#10 +
           'https://developer.microsoft.com/microsoft-edge/webview2/', mbInformation, MB_OK);
end;
