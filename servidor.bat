@echo off
rem ==========================================================================
rem  Servidor WYD 7.87 (Go): DBSRV, TMSRV e painel admin.
rem
rem    servidor                 compila e inicia tudo (o mesmo que "start")
rem    servidor start           Postgres (Docker, se preciso) + compila + DBSRV,
rem                             TMSRV e admin, cada um na sua janela
rem    servidor build           so compila (bin\dbsrv.exe, tmsrv.exe, admin.exe)
rem    servidor stop            fecha DBSRV, TMSRV e admin
rem    servidor restart         stop + start
rem    servidor dbsrv|tmsrv|admin   compila e inicia so aquele, nesta janela
rem    servidor db              sobe so o Postgres (docker compose up -d)
rem    servidor senha-admin     mostra a senha gerada do painel
rem
rem  Configuracao: variaveis do arquivo .env (copie de .env.example), por
rem  exemplo DATABASE_URL, ADMIN_USER, ADMIN_PASSWORD_HASH,
rem  DBSRV_MASTER_PASSWORD_HASH. Opcoes extras de cada programa:
rem  DBSRV_ARGS, TMSRV_ARGS, ADMIN_ARGS (ex.: set TMSRV_ARGS=-debug).
rem ==========================================================================
setlocal EnableExtensions
cd /d "%~dp0"
set "ROOT=%~dp0"
set "BIN=%ROOT%bin"

rem Go instalado fora do PATH (instalacao padrao de 32 bits).
where go >nul 2>nul || set "PATH=%PATH%;C:\Program Files\Go\bin;C:\Program Files (x86)\Go\bin"

rem Valores padrao, sobrescritos pelo .env.
set "DATABASE_URL=postgres://dbsrv:dbsrv@127.0.0.1:5432/dbsrv"
set "ADMIN_LISTEN=127.0.0.1:8080"
rem Porta publica do admin para o cliente do jogo: serv00.htm (contagem de
rem online por canal, URL em serverlist.bin) e marcas de guild. Vazio = desliga.
set "PUBLIC_LISTEN=:80"
if exist ".env" (
  for /f "usebackq eol=# tokens=1* delims==" %%a in (".env") do if not "%%b"=="" set "%%a=%%b"
)

set "CMD=%~1"
if "%CMD%"=="" set "CMD=start"
set "ADMIN_PUBLIC="
if not "%PUBLIC_LISTEN%"=="" set "ADMIN_PUBLIC=-public-listen %PUBLIC_LISTEN%"

if /i "%CMD%"=="build"       goto :build_only
if /i "%CMD%"=="start"       goto :start
if /i "%CMD%"=="stop"        goto :stop
if /i "%CMD%"=="restart"     ( call :stop_all & goto :start )
if /i "%CMD%"=="db"          ( call :database & goto :eof )
if /i "%CMD%"=="dbsrv"       goto :one
if /i "%CMD%"=="tmsrv"       goto :one
if /i "%CMD%"=="admin"       goto :one
if /i "%CMD%"=="senha-admin" goto :admin_password
echo Comando desconhecido: %CMD%
echo Use: servidor [start^|build^|stop^|restart^|dbsrv^|tmsrv^|admin^|db^|senha-admin]
exit /b 1

rem --------------------------------------------------------------------------
:build_only
call :build || exit /b 1
echo Pronto: %BIN%
exit /b 0

:start
call :database || goto :fail
call :build || goto :fail
call :running dbsrv.exe && echo DBSRV ja esta rodando. || (
  start "DBSRV" /D "%ROOT%" cmd /k ""%BIN%\dbsrv.exe" %DBSRV_ARGS%"
  call :wait_port 7514 "DBSRV" || goto :fail
)
call :running tmsrv.exe && echo TMSRV ja esta rodando. || (
  start "TMSRV" /D "%ROOT%" cmd /k ""%BIN%\tmsrv.exe" %TMSRV_ARGS%"
)
call :running admin.exe && echo Painel admin ja esta rodando. || (
  start "ADMIN" /D "%ROOT%" cmd /k ""%BIN%\admin.exe" -listen %ADMIN_LISTEN% %ADMIN_PUBLIC% %ADMIN_ARGS%"
)
echo.
echo Servidor iniciado. Painel admin: http://%ADMIN_LISTEN%
if defined ADMIN_PUBLIC echo Lista de canais do cliente: http://^<este IP^>%PUBLIC_LISTEN%/serv00.htm
if not defined ADMIN_PASSWORD_HASH echo Senha do painel: servidor senha-admin
exit /b 0

:one
call :build || exit /b 1
if /i "%CMD%"=="dbsrv" "%BIN%\dbsrv.exe" %DBSRV_ARGS%
if /i "%CMD%"=="tmsrv" "%BIN%\tmsrv.exe" %TMSRV_ARGS%
if /i "%CMD%"=="admin" "%BIN%\admin.exe" -listen %ADMIN_LISTEN% %ADMIN_PUBLIC% %ADMIN_ARGS%
exit /b %errorlevel%

:stop
call :stop_all
exit /b 0

:admin_password
if exist ".admin-password" (
  type ".admin-password"
) else (
  echo Ainda nao ha senha gerada: inicie o painel uma vez ^(ou defina ADMIN_PASSWORD_HASH no .env^).
)
exit /b 0

:fail
echo.
echo Falhou. Veja as mensagens acima.
exit /b 1

rem ---- sub-rotinas ----------------------------------------------------------

:build
where go >nul 2>nul || ( echo Go nao encontrado. Instale o Go ou ponha go.exe no PATH. & exit /b 1 )
echo Compilando...
if not exist "%BIN%" mkdir "%BIN%"
for %%p in (dbsrv tmsrv admin) do (
  go build -trimpath -o "%BIN%\%%p.new.exe" ./cmd/%%p || ( echo Erro ao compilar %%p. & exit /b 1 )
  move /y "%BIN%\%%p.new.exe" "%BIN%\%%p.exe" >nul 2>nul || (
    del "%BIN%\%%p.new.exe" >nul 2>nul
    echo   bin\%%p.exe esta em uso: mantido o atual ^(use "servidor restart" para trocar^).
  )
)
exit /b 0

:database
call :port_open 5432 && exit /b 0
where docker >nul 2>nul || ( echo Postgres nao responde em 127.0.0.1:5432 e o Docker nao esta instalado. & exit /b 1 )
echo Subindo o Postgres ^(docker compose up -d^)...
docker compose up -d || ( echo Nao foi possivel subir o Postgres. O Docker Desktop esta aberto? & exit /b 1 )
call :wait_port 5432 "Postgres"
exit /b %errorlevel%

:stop_all
rem A TMSRV primeiro: ela salva os jogadores no DBSRV ao fechar.
call :close TMSRV tmsrv.exe 20
call :close DBSRV dbsrv.exe 10
call :close ADMIN admin.exe 5
exit /b 0

rem :close <titulo da janela> <exe> <segundos>: fecha a janela (o programa
rem recebe o aviso e encerra limpo) e so forca se nao sair no prazo.
:close
call :running %~2 || exit /b 0
echo Fechando %~1...
powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT%scripts\ctrl-c.ps1" %~n2 >nul 2>nul
for /l %%i in (1,1,%~3) do (
  call :running %~2 || exit /b 0
  timeout /t 1 /nobreak >nul
)
echo   %~1 nao fechou em %~3 s; encerrando a forca.
taskkill /F /IM %~2 /T >nul 2>nul
exit /b 0

:running
tasklist /FI "IMAGENAME eq %~1" 2>nul | find /i "%~1" >nul
exit /b %errorlevel%

:port_open
powershell -NoProfile -Command "try { $c = New-Object Net.Sockets.TcpClient; $c.Connect('127.0.0.1', %~1); $c.Close(); exit 0 } catch { exit 1 }" >nul 2>nul
exit /b %errorlevel%

:wait_port
echo Aguardando %~2 na porta %~1...
for /l %%i in (1,1,30) do (
  call :port_open %~1 && exit /b 0
  timeout /t 1 /nobreak >nul
)
echo %~2 nao abriu a porta %~1 em 30 segundos.
exit /b 1
