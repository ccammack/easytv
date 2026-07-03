#!/usr/bin/env elixir

Mix.install([
  {:input_event, "~> 1.4"},
  {:jason, "~> 1.4"}
])

require Logger

Logger.configure(level: :debug)

Logger.configure_backend(:console,
  format: "[$level] $message\n",
  metadata: []
)

defmodule Script do
  def name do
    __ENV__.file
    |> Path.basename()
    |> Path.rootname()
  end

  def state_home do
    case System.get_env("XDG_STATE_HOME") do
      nil -> Path.join(System.user_home!(), ".local/state")
      "" -> Path.join(System.user_home!(), ".local/state")
      path -> path
    end
  end

  def state_dir, do: Path.join(state_home(), name())
  def state_path(file), do: Path.join(state_dir(), file)

  def tmp_file(suffix) when is_binary(suffix) do
    tmp_file([suffix])
  end

  def tmp_file(segments) when is_list(segments) do
    path = Path.join([System.tmp_dir!(), name() | segments])
    File.mkdir_p!(Path.dirname(path))
    path
  end

  def tmp_dir(suffix) when is_binary(suffix) do
    tmp_dir([suffix])
  end

  def tmp_dir(segments) when is_list(segments) do
    dir = Path.join([System.tmp_dir!(), name() | segments])
    File.mkdir_p!(dir)
    dir
  end
end

defmodule MediaTypes do
  @audio_extensions MapSet.new([
                      ".mp3",
                      ".flac",
                      ".m4a",
                      ".m4b",
                      ".aac",
                      ".ac3",
                      ".ogg",
                      ".oga",
                      ".opus",
                      ".wav",
                      ".aiff",
                      ".alac",
                      ".ape",
                      ".amr",
                      ".wma",
                      ".wv",
                      ".tta",
                      ".mp1",
                      ".mp2",
                      ".mpa",
                      ".mid",
                      ".midi",
                      ".xm",
                      ".it",
                      ".s3m",
                      ".mod",
                      ".ra",
                      ".rmi",
                      ".spx",
                      ".voc"
                    ])

  @video_extensions MapSet.new([
                      ".rm",
                      ".mp4",
                      ".m4v",
                      ".mkv",
                      ".mov",
                      ".avi",
                      ".webm",
                      ".flv",
                      ".wmv",
                      ".mpg",
                      ".mpeg",
                      ".mpv",
                      ".m1v",
                      ".m2v",
                      ".ts",
                      ".m2ts",
                      ".mts",
                      ".vob",
                      ".ogv",
                      ".ogm",
                      ".3gp",
                      ".3g2",
                      ".asf",
                      ".divx",
                      ".dv",
                      ".f4v",
                      ".mxf",
                      ".nuv",
                      ".rmvb",
                      ".tod",
                      ".trp",
                      ".vro"
                    ])

  @image_extensions MapSet.new([
                      ".jpg",
                      ".jpeg",
                      ".jpe",
                      ".jfif",
                      ".jif",
                      ".png",
                      ".gif",
                      ".bmp",
                      ".tif",
                      ".tiff",
                      ".webp",
                      ".ico",
                      ".pbm",
                      ".pgm",
                      ".ppm",
                      ".pnm",
                      ".tga"
                    ])

  @media_extensions MapSet.union(@audio_extensions, @video_extensions)
                    |> MapSet.union(@image_extensions)

  def image_extensions, do: @image_extensions
  def media_extensions, do: @media_extensions
end

defmodule DirectoryPlaylistLoader do
  require Logger

  def scan_root(root_dir) do
    root_dir = Path.expand(root_dir)

    case File.ls(root_dir) do
      {:ok, entries} ->
        channels =
          entries
          |> Enum.reject(&String.starts_with?(&1, "."))
          |> Enum.map(&Path.join(root_dir, &1))
          |> Enum.filter(&File.dir?/1)
          |> Enum.sort()
          |> Enum.reduce(%{}, fn channel_path, acc ->
            channel_name = Path.basename(channel_path)

            media =
              channel_path
              |> list_files_recursive()
              |> Enum.filter(&media_file?/1)
              |> Enum.sort()

            if media == [] do
              acc
            else
              Map.put(acc, channel_path, %{
                "name" => channel_name,
                "path" => channel_path,
                "media" => media
              })
            end
          end)

        {:ok, channels}

      {:error, reason} ->
        Logger.warning("Could not list root directory #{root_dir}: #{inspect(reason)}")
        {:error, reason}
    end
  end

  defp list_files_recursive(path) do
    cond do
      File.regular?(path) ->
        [path]

      File.dir?(path) ->
        case File.ls(path) do
          {:ok, entries} ->
            entries
            |> Enum.reject(&String.starts_with?(&1, "."))
            |> Enum.map(&Path.join(path, &1))
            |> Enum.flat_map(&list_files_recursive/1)

          {:error, reason} ->
            Logger.warning("Could not list directory #{path}: #{inspect(reason)}")
            []
        end

      true ->
        []
    end
  end

  defp media_file?(path) do
    path
    |> Path.extname()
    |> String.downcase()
    |> then(&MapSet.member?(MediaTypes.media_extensions(), &1))
  end
end

defmodule JsonFileStore do
  def load!(path, default, validate!, label)
      when is_binary(path) and is_function(validate!, 1) and is_binary(label) do
    case File.read(path) do
      {:ok, contents} ->
        case Jason.decode(contents) do
          {:ok, data} ->
            validate!.(data)

          {:error, reason} ->
            raise "Could not parse #{label} #{path}: #{inspect(reason)}"
        end

      {:error, :enoent} ->
        default

      {:error, reason} ->
        raise "Could not read #{label} #{path}: #{inspect(reason)}"
    end
  end

  def save!(path, data, validate!) when is_binary(path) and is_function(validate!, 1) do
    validate!.(data)
    dir = Path.dirname(path)
    File.mkdir_p!(dir)

    json = Jason.encode_to_iodata!(data, pretty: true)
    tmp = path <> ".tmp"

    File.write!(tmp, json)

    case File.rename(tmp, path) do
      :ok ->
        :ok

      {:error, reason} ->
        # Log and keep going instead of crashing the whole app
        IO.puts("JsonFileStore.save!: rename failed from #{tmp} to #{path}: #{inspect(reason)}")

        # Best-effort fallback: try a second time after re-creating dir
        File.mkdir_p!(dir)

        case File.rename(tmp, path) do
          :ok ->
            :ok

          {:error, reason2} ->
            IO.puts(
              "JsonFileStore.save!: second rename failed from #{tmp} to #{path}: #{inspect(reason2)}"
            )

            # Leave the .tmp file; caller can decide if this is fatal
            :error
        end
    end
  end
end

defmodule CatalogStore do
  @catalog_file "catalog.json"
  @version 1

  def load! do
    JsonFileStore.load!(
      catalog_path(),
      default_catalog(),
      &validate_catalog!/1,
      "catalog file"
    )
  end

  def save!(catalog_map) when is_map(catalog_map) do
    JsonFileStore.save!(catalog_path(), catalog_map, &validate_catalog!/1)
  end

  def validate_catalog!(%{"version" => @version, "roots" => roots} = data)
      when is_map(roots),
      do: data

  def validate_catalog!(data),
    do: raise("Invalid catalog file schema: #{inspect(data, pretty: true)}")

  def default_catalog, do: %{"version" => @version, "roots" => %{}}
  def catalog_path, do: Script.state_path(@catalog_file)
end

defmodule StateStore do
  @state_file "state.json"
  @version 1

  def load! do
    JsonFileStore.load!(
      state_path(),
      default_state(),
      &validate_state!/1,
      "state file"
    )
  end

  def save!(state_map) when is_map(state_map) do
    JsonFileStore.save!(state_path(), state_map, &validate_state!/1)
  end

  def validate_state!(
        %{
          "version" => @version,
          "current_channel_id" => current_channel_id,
          "channel_state" => channel_state
        } = data
      )
      when (is_binary(current_channel_id) or is_nil(current_channel_id)) and is_map(channel_state),
      do: data

  def validate_state!(data),
    do: raise("Invalid state file schema: #{inspect(data, pretty: true)}")

  def default_state,
    do: %{"version" => @version, "current_channel_id" => nil, "channel_state" => %{}}

  def state_path, do: Script.state_path(@state_file)
end

defmodule PreparedMedia do
  require Logger

  @max_image_dimension 1920
  @keep_last_files 4

  def prepare_for_playback(path) when is_binary(path) do
    abspath = Path.expand(path)

    if image_file?(abspath) do
      prepare_image_for_display(abspath, @max_image_dimension)
    else
      {:ok, %{original_path: abspath, play_path: abspath, tmp?: false, downscaled?: false}}
    end
  end

  def image_file?(path) when is_binary(path) do
    path
    |> Path.extname()
    |> String.downcase()
    |> then(&MapSet.member?(MediaTypes.image_extensions(), &1))
  end

  def image_file?(_), do: false

  defp prepare_image_for_display(path, maxdim) do
    tmp_dir = Script.tmp_dir("prepared-media")

    result =
      case copy_image_to_tmp_source(path, tmp_dir) do
        {:ok, tmp_source} ->
          with {:ok, w, h} <- image_dimensions(tmp_source) do
            if max(w, h) > maxdim do
              case downscale_to_output(tmp_source, tmp_dir, maxdim) do
                {:ok, tmp_output} ->
                  {:ok,
                   %{
                     original_path: path,
                     play_path: tmp_output,
                     tmp?: true,
                     downscaled?: true
                   }}

                {:error, reason} ->
                  {:error, reason}
              end
            else
              {:ok,
               %{
                 original_path: path,
                 play_path: tmp_source,
                 tmp?: true,
                 downscaled?: false
               }}
            end
          end

        {:error, reason} ->
          {:error, reason}
      end

    maybe_start_async_sweep(tmp_dir, @keep_last_files)
    result
  end

  defp copy_image_to_tmp_source(path, tmp_dir) do
    ext =
      path
      |> Path.extname()
      |> String.downcase()

    tmp_source =
      Path.join(
        tmp_dir,
        "src-#{System.system_time(:millisecond)}-#{System.unique_integer([:positive])}#{ext}"
      )

    Logger.debug(
      "PreparedMedia copying image source=#{inspect(path)} tmp_source=#{inspect(tmp_source)}"
    )

    case File.cp(path, tmp_source) do
      :ok -> {:ok, tmp_source}
      {:error, reason} -> {:error, {:file_copy_failed, path, tmp_source, reason}}
    end
  end

  defp image_dimensions(path) do
    vipsheader = System.find_executable("vipsheader")

    if is_nil(vipsheader) do
      {:error, :vipsheader_not_found}
    else
      with {:ok, w} <- vipsheader_int(vipsheader, "width", path),
           {:ok, h} <- vipsheader_int(vipsheader, "height", path) do
        {:ok, w, h}
      end
    end
  end

  defp vipsheader_int(vipsheader, field, path) do
    {output, status} =
      System.cmd(vipsheader, ["-f", field, path], stderr_to_stdout: true)

    case {status, String.trim(output)} do
      {0, value} ->
        case Integer.parse(value) do
          {n, ""} -> {:ok, n}
          _ -> {:error, {:bad_vipsheader_output, field, value}}
        end

      _ ->
        {:error, {:vipsheader_failed, field, output}}
    end
  end

  defp downscale_to_output(tmp_source, tmp_dir, max_dim) do
    vipsthumbnail = System.find_executable("vipsthumbnail")

    if is_nil(vipsthumbnail) do
      {:error, :vipsthumbnail_not_found}
    else
      tmp_output =
        Path.join(
          tmp_dir,
          "out-#{System.system_time(:millisecond)}-#{System.unique_integer([:positive])}.jpg"
        )

      {output, status} =
        System.cmd(
          vipsthumbnail,
          [
            tmp_source,
            "--size",
            "#{max_dim}x#{max_dim}",
            "--linear",
            "-o",
            "#{tmp_output}[Q=90]"
          ],
          stderr_to_stdout: true
        )

      case status do
        0 ->
          if File.regular?(tmp_output) do
            {:ok, tmp_output}
          else
            {:error, {:vipsthumbnail_missing_output, output}}
          end

        _ ->
          {:error, {:vipsthumbnail_failed, output}}
      end
    end
  end

  defp maybe_start_async_sweep(dir, keep_last) do
    Task.start(fn ->
      sweep_tmp_dir(dir, keep_last)
    end)

    :ok
  end

  defp sweep_tmp_dir(dir, keep_last)
       when is_binary(dir) and is_integer(keep_last) and keep_last >= 0 do
    case File.ls(dir) do
      {:ok, entries} ->
        entries
        |> Enum.map(&Path.join(dir, &1))
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort_by(&mtime/1, :desc)
        |> Enum.drop(keep_last)
        |> Enum.each(fn path ->
          case File.rm(path) do
            :ok ->
              :ok

            {:error, :enoent} ->
              :ok

            {:error, reason} ->
              Logger.debug(
                "PreparedMedia sweep delete failed path=#{inspect(path)} reason=#{inspect(reason)}"
              )
          end
        end)

        :ok

      {:error, reason} ->
        Logger.debug(
          "PreparedMedia sweep list failed dir=#{inspect(dir)} reason=#{inspect(reason)}"
        )

        :ok
    end
  end

  defp mtime(path) do
    case File.stat(path) do
      {:ok, %File.Stat{mtime: mtime}} -> mtime
      _ -> {{1970, 1, 1}, {0, 0, 0}}
    end
  end
end

defmodule MpvPlayer do
  use GenServer
  require Logger

  @poll_interval 1_000
  @connect_retry_ms 200
  @startup_timeout_ms 5_000
  @request_timeout_ms 3_000

  defmodule State do
    defstruct [
      :parent,
      :port,
      :port_os_pid,
      :socket,
      :socket_path,
      :file,
      :start_pos,
      :connected?,
      :stopping?,
      :last_position,
      :buffer,
      ended?: false,
      request_id: 1,
      pending: %{}
    ]
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)
  def pause(server), do: GenServer.call(server, :pause)
  def resume(server), do: GenServer.call(server, :resume)
  def stop(server), do: GenServer.cast(server, :stop)

  @impl true
  def init(opts) do
    parent = Keyword.fetch!(opts, :parent)
    file = Keyword.fetch!(opts, :file)
    start_pos = Keyword.get(opts, :start_pos, 0)
    socket_path = socket_path()

    case File.rm(socket_path) do
      :ok ->
        :ok

      {:error, :enoent} ->
        :ok

      {:error, reason} ->
        Logger.warning("could not remove stale socket #{socket_path}: #{inspect(reason)}")
    end

    image_file? = PreparedMedia.image_file?(file)

    base_argv = [
      "--no-terminal",
      "--idle=no",
      "--force-window=yes",
      "--fullscreen",
      "--no-input-default-bindings",
      "--ao=alsa",
      "--input-ipc-server=#{socket_path}",
      "--title=#{Script.name()}",
      "--osd-level=0",
      "--cache=no",
      "--profile=fast",
      "--hwdec=auto"
    ]

    playback_argv =
      if image_file? do
        [
          "--keep-open=yes",
          "--image-display-duration=86400"
        ]
      else
        [
          "--keep-open=no"
        ]
      end

    start_argv =
      if start_pos > 0 and not image_file? do
        ["--start=#{start_pos}"]
      else
        []
      end

    argv = base_argv ++ playback_argv ++ start_argv ++ [file]

    port =
      Port.open({:spawn_executable, find_executable!("mpv")}, [
        :binary,
        :exit_status,
        :stderr_to_stdout,
        :use_stdio,
        :hide,
        args: argv
      ])

    port_os_pid =
      case Port.info(port, :os_pid) do
        {:os_pid, pid} -> pid
        _ -> nil
      end

    Logger.warning(
      "mpv init: wrapper_pid=#{inspect(self())} parent=#{inspect(parent)} port=#{inspect(port)} port_os_pid=#{inspect(port_os_pid)} socket_path=#{socket_path} file=#{inspect(file)} start_pos=#{inspect(start_pos)}"
    )

    if port_os_pid do
      log_process_snapshot("mpv init process snapshot", port_os_pid)
    end

    state = %State{
      parent: parent,
      port: port,
      port_os_pid: port_os_pid,
      socket_path: socket_path,
      file: file,
      start_pos: start_pos,
      connected?: false,
      stopping?: false,
      last_position: nil,
      buffer: ""
    }

    case connect_until(socket_path, @startup_timeout_ms) do
      {:ok, socket} ->
        Logger.info(
          "mpv started wrapper_pid=#{inspect(self())} port=#{inspect(port)} os_pid=#{inspect(port_os_pid)} socket=#{socket_path}"
        )

        send_event(state, :started, %{
          os_pid: port_os_pid,
          socket_path: socket_path,
          file: file,
          wrapper_pid: self(),
          port: inspect(port)
        })

        state = %{state | socket: socket, connected?: true}
        state = send_observe_property(state, 2, "pause")

        Process.send_after(self(), :poll_status, @poll_interval)
        {:ok, state}

      {:error, reason} ->
        Logger.error(
          "failed to connect to mpv ipc wrapper_pid=#{inspect(self())} port=#{inspect(port)} os_pid=#{inspect(port_os_pid)} reason=#{inspect(reason)}"
        )

        send_event(state, :ipc_error, %{stage: :connect, reason: inspect(reason)})
        {:stop, {:ipc_connect_failed, reason}, state}
    end
  end

  @impl true
  def handle_call(:pause, _from, state) do
    {result, state} = send_command(state, ["set_property", "pause", true], :pause)
    {:reply, result, state}
  end

  def handle_call(:resume, _from, state) do
    {result, state} = send_command(state, ["set_property", "pause", false], :resume)
    {:reply, result, state}
  end

  @impl true
  def handle_cast(:stop, %{stopping?: true} = state) do
    Logger.warning(
      "mpv stop: already stopping wrapper_pid=#{inspect(self())} port=#{inspect(state.port)} os_pid=#{inspect(state.port_os_pid)} file=#{inspect(state.file)}"
    )

    {:noreply, state}
  end

  def handle_cast(:stop, state) do
    state = %{state | stopping?: true}

    Logger.warning(
      "mpv stop: begin wrapper_pid=#{inspect(self())} port=#{inspect(state.port)} os_pid=#{inspect(state.port_os_pid)} socket=#{inspect(state.socket_path)} connected?=#{inspect(state.connected?)} file=#{inspect(state.file)}"
    )

    if state.port_os_pid do
      log_process_snapshot("mpv stop before kill", state.port_os_pid)
    end

    force_kill_port_os_pid(state)

    if state.port_os_pid do
      log_process_snapshot("mpv stop after kill", state.port_os_pid)
    end

    {:stop, :normal, state}
  end

  @impl true
  def handle_info(:force_kill_if_needed, %{stopping?: true} = state) do
    Logger.warning(
      "mpv stop grace expired; force killing os_pid=#{inspect(state.port_os_pid)} file=#{inspect(state.file)}"
    )

    force_kill_port_os_pid(state)
    {:noreply, state}
  end

  def handle_info(:force_kill_if_needed, state), do: {:noreply, state}

  def handle_info(:poll_status, state) do
    if state.connected? and not state.stopping? do
      {_result, state} = send_command(state, ["get_property", "time-pos"], :poll_time_pos)
      Process.send_after(self(), :poll_status, @poll_interval)
      {:noreply, state}
    else
      {:noreply, state}
    end
  end

  def handle_info({:request_timeout, request_id}, state) do
    case Map.pop(state.pending, request_id) do
      {nil, _pending} ->
        {:noreply, state}

      {%{tag: tag, command: command, sent_at_ms: sent_at_ms}, pending} ->
        age_ms = System.monotonic_time(:millisecond) - sent_at_ms

        # Logger.error(
        #   "mpv request timed out request_id=#{request_id} tag=#{inspect(tag)} age_ms=#{age_ms} command=#{inspect(command)}"
        # )

        if state.stopping? do
          Logger.debug(
            "mpv request timed out during stop request_id=#{request_id} tag=#{inspect(tag)} age_ms=#{age_ms} command=#{inspect(command)}"
          )
        else
          Logger.error(
            "mpv request timed out request_id=#{request_id} tag=#{inspect(tag)} age_ms=#{age_ms} command=#{inspect(command)}"
          )
        end

        send_event(state, :request_timeout, %{
          request_id: request_id,
          tag: tag,
          command: command,
          age_ms: age_ms
        })

        {:noreply, %{state | pending: pending}}
    end
  end

  def handle_info({port, {:data, data}}, %{port: port} = state) do
    text = String.trim(data)

    if text != "" do
      Logger.debug("mpv output: #{text}")
      send_event(state, :log, %{source: :mpv_stdout, text: text})
    end

    {:noreply, state}
  end

  def handle_info({port, {:exit_status, status}}, %{port: port} = state) do
    Logger.warning(
      "mpv exit_status: wrapper_pid=#{inspect(self())} port=#{inspect(port)} os_pid=#{inspect(state.port_os_pid)} status=#{status} stopping?=#{inspect(state.stopping?)} file=#{inspect(state.file)}"
    )

    if state.port_os_pid do
      log_process_snapshot("mpv exit_status snapshot", state.port_os_pid)
    end

    send_event(state, :process_exit, %{status: status, stopping?: state.stopping?})

    {:stop, :normal, state}
  end

  def handle_info({:tcp, socket, data}, %{socket: socket} = state) do
    :ok = :inet.setopts(socket, active: :once)
    {:noreply, consume_socket_data(%{state | buffer: state.buffer <> data})}
  end

  def handle_info({:tcp_closed, socket}, %{socket: socket} = state) do
    expected? = state.stopping? or state.ended?

    if expected? do
      Logger.debug("mpv ipc socket closed")
    else
      Logger.error("mpv ipc socket closed")
      send_event(state, :ipc_error, %{stage: :socket_closed})
    end

    {:stop, :normal, %{state | connected?: false, socket: nil, pending: %{}}}
  end

  def handle_info({:tcp_error, socket, reason}, %{socket: socket} = state) do
    Logger.error("mpv ipc socket error: #{inspect(reason)}")
    send_event(state, :ipc_error, %{stage: :socket_error, reason: inspect(reason)})

    {:stop, :normal, %{state | connected?: false, socket: nil, pending: %{}}}
  end

  def handle_info(msg, state) do
    Logger.debug("unhandled message: #{inspect(msg)}")
    {:noreply, state}
  end

  @impl true
  def terminate(reason, state) do
    Logger.warning(
      "MpvPlayer terminate: wrapper_pid=#{inspect(self())} reason=#{inspect(reason)} port=#{inspect(state.port)} os_pid=#{inspect(state.port_os_pid)} socket=#{inspect(state.socket_path)} stopping?=#{inspect(state.stopping?)} file=#{inspect(state.file)}"
    )

    if state.port_os_pid do
      log_process_snapshot("terminate before cleanup", state.port_os_pid)
    end

    force_kill_port_os_pid(state)

    if state.port_os_pid do
      log_process_snapshot("terminate after cleanup", state.port_os_pid)
    end

    if state.socket, do: safe_socket_close(state.socket)

    if state.socket_path do
      case File.rm(state.socket_path) do
        :ok -> :ok
        {:error, :enoent} -> :ok
        {:error, _} -> :ok
      end
    end

    :ok
  end

  defp log_process_snapshot(label, os_pid) do
    try do
      {output, status} =
        System.cmd(
          "ps",
          ["-o", "pid,ppid,pgid,stat,etime,command=", "-p", Integer.to_string(os_pid)],
          stderr_to_stdout: true
        )

      Logger.warning(
        "#{label}: os_pid=#{os_pid} ps_status=#{status} ps_output=#{inspect(String.trim(output))}"
      )
    catch
      kind, reason ->
        Logger.warning("#{label}: os_pid=#{os_pid} ps raised #{inspect({kind, reason})}")
    end
  end

  defp socket_path do
    Script.tmp_file(
      "mpv-player-#{System.os_time(:millisecond)}-#{:erlang.unique_integer([:positive])}.sock"
    )
  end

  defp find_executable!(name) do
    System.find_executable(name) || raise "could not find executable: #{name}"
  end

  defp connect_until(_socket_path, remaining) when remaining <= 0, do: {:error, :timeout}

  defp connect_until(socket_path, remaining) do
    case :gen_tcp.connect(
           {:local, String.to_charlist(socket_path)},
           0,
           [:binary, active: :once],
           @connect_retry_ms
         ) do
      {:ok, socket} ->
        {:ok, socket}

      {:error, _reason} ->
        Process.sleep(@connect_retry_ms)
        connect_until(socket_path, remaining - @connect_retry_ms)
    end
  end

  defp send_command(%{socket: nil} = state, command, tag) do
    send_event(state, :ipc_error, %{
      stage: :send_command,
      command: command,
      tag: tag,
      reason: :no_socket
    })

    {{:error, :no_socket}, state}
  end

  defp send_command(state, command, tag) do
    request_id = state.request_id
    payload = Jason.encode!(%{command: command, request_id: request_id}) <> "\n"

    case :gen_tcp.send(state.socket, payload) do
      :ok ->
        timer_ref =
          Process.send_after(self(), {:request_timeout, request_id}, @request_timeout_ms)

        pending_entry = %{
          tag: tag,
          command: command,
          sent_at_ms: System.monotonic_time(:millisecond),
          timer_ref: timer_ref
        }

        state =
          state
          |> put_in([Access.key(:pending), request_id], pending_entry)
          |> Map.put(:request_id, request_id + 1)

        {{:ok, request_id}, state}

      {:error, reason} ->
        send_event(state, :ipc_error, %{
          stage: :send_command,
          command: command,
          tag: tag,
          reason: inspect(reason)
        })

        {{:error, reason}, state}
    end
  end

  defp send_observe_property(state, id, name) do
    {_result, state} =
      send_command(state, ["observe_property", id, name], {:observe_property, name})

    state
  end

  defp consume_socket_data(state) do
    parts = String.split(state.buffer, "\n", trim: false)
    complete_lines = Enum.drop(parts, -1)
    tail = List.last(parts) || ""
    state = %{state | buffer: tail}

    Enum.reduce(complete_lines, state, fn line, acc ->
      if String.trim(line) == "" do
        acc
      else
        handle_json_line(acc, line)
      end
    end)
  end

  defp handle_json_line(state, line) do
    case Jason.decode(line) do
      {:ok, %{"event" => "property-change", "name" => "pause", "data" => paused}} ->
        send_event(state, :pause_changed, %{paused: paused})
        state

      {:ok, %{"event" => "end-file", "reason" => reason} = msg} ->
        send(state.parent, {:mpv_end_of_file, self(), reason})
        send_event(state, :end_file, msg)
        %{state | ended?: true}

      {:ok, %{"request_id" => request_id, "data" => seconds, "error" => "success"}}
      when is_integer(request_id) and is_number(seconds) ->
        case Map.pop(state.pending, request_id) do
          {%{tag: :poll_time_pos, timer_ref: timer_ref}, pending} ->
            Process.cancel_timer(timer_ref)

            if state.last_position != seconds do
              send(state.parent, {:mpv_position, self(), seconds})
            end

            %{state | last_position: seconds, pending: pending}

          {%{tag: tag, command: command, timer_ref: timer_ref}, pending} ->
            Process.cancel_timer(timer_ref)

            send_event(state, :reply, %{
              request_id: request_id,
              tag: tag,
              command: command,
              data: seconds
            })

            %{state | pending: pending}

          {nil, _pending} ->
            state
        end

      {:ok, %{"request_id" => request_id, "error" => "success"} = msg}
      when is_integer(request_id) ->
        case Map.pop(state.pending, request_id) do
          {%{tag: tag, command: command, timer_ref: timer_ref}, pending} ->
            Process.cancel_timer(timer_ref)

            send_event(state, :reply, %{
              request_id: request_id,
              tag: tag,
              command: command,
              reply: msg
            })

            %{state | pending: pending}

          {nil, _pending} ->
            state
        end

      {:ok, %{"request_id" => request_id, "error" => error} = msg} when is_integer(request_id) ->
        case Map.pop(state.pending, request_id) do
          {%{tag: tag, command: command, timer_ref: timer_ref}, pending} ->
            Process.cancel_timer(timer_ref)

            send_event(state, :command_error, %{
              request_id: request_id,
              tag: tag,
              command: command,
              error: error,
              reply: msg
            })

            %{state | pending: pending}

          {nil, _pending} ->
            state
        end

      {:ok, decoded} ->
        send_event(state, :ipc_message, decoded)
        state

      {:error, reason} ->
        send_event(state, :ipc_decode_error, %{reason: inspect(reason), line: line})
        state
    end
  end

  defp send_event(state, kind, payload) do
    send(state.parent, {:mpv_event, self(), kind, payload})
  end

  defp safe_socket_close(socket) do
    try do
      :gen_tcp.close(socket)
    catch
      _, _ -> :ok
    end

    :ok
  end

  defp force_kill_port_os_pid(%{port_os_pid: nil} = state) do
    Logger.warning(
      "force_kill_port_os_pid: no os_pid wrapper_pid=#{inspect(self())} port=#{inspect(state.port)} file=#{inspect(state.file)}"
    )

    :ok
  end

  defp force_kill_port_os_pid(%{port: port, port_os_pid: os_pid} = state) do
    Logger.warning(
      "force_kill_port_os_pid: wrapper_pid=#{inspect(self())} port=#{inspect(port)} os_pid=#{inspect(os_pid)} file=#{inspect(state.file)}"
    )

    log_process_snapshot("force_kill pre", os_pid)

    try do
      {kill_output, kill_status} =
        System.cmd("kill", ["-KILL", Integer.to_string(os_pid)], stderr_to_stdout: true)

      Logger.warning(
        "force_kill_port_os_pid: kill pid=#{os_pid} status=#{kill_status} output=#{inspect(kill_output)}"
      )
    catch
      kind, reason ->
        Logger.warning("force_kill_port_os_pid: kill raised #{inspect({kind, reason})}")
    end

    log_process_snapshot("force_kill post", os_pid)

    if port do
      try do
        Logger.warning("force_kill_port_os_pid: closing port #{inspect(port)}")
        Port.close(port)
      catch
        kind, reason ->
          Logger.warning("force_kill_port_os_pid: Port.close raised #{inspect({kind, reason})}")
      end
    end

    :ok
  end
end

defmodule InputDevice do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name))
  end

  @impl true
  def init(opts) do
    receiver = Keyword.fetch!(opts, :receiver)
    path = Keyword.fetch!(opts, :path)
    {:ok, _pid} = InputEvent.start_link(path: path, receiver: receiver)
    {:ok, %{path: path, receiver: receiver}}
  end
end

defmodule InputDevices do
  require Logger

  def keyboard_paths do
    by_id_paths =
      "/dev/input/by-id/*-event-kbd"
      |> Path.wildcard()
      |> Enum.filter(&File.exists?/1)
      |> Enum.sort()

    event_paths =
      if by_id_paths == [] do
        "/dev/input/event*"
        |> Path.wildcard()
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort()
      else
        []
      end

    paths = by_id_paths ++ event_paths

    if paths == [] do
      raise "No keyboard input devices found."
    end

    Logger.warning("keyboard_paths=#{inspect(paths)}")
    paths
  end
end

defmodule RuntimeSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    receiver = Keyword.fetch!(opts, :receiver)

    input_children =
      InputDevices.keyboard_paths()
      |> Enum.map(fn path ->
        Supervisor.child_spec({InputDevice, [path: path, receiver: receiver]},
          id: {:input_device, path}
        )
      end)

    children = input_children

    Supervisor.init(children, strategy: :one_for_one)
  end
end

defmodule Runner do
  require Logger

  @task_supervisor App.TaskSupervisor

  def run(cmd, args \\ [], opts \\ [])

  def run(cmd, args, opts) when is_binary(cmd) and is_list(args) and is_list(opts) do
    with :ok <- validate(cmd, args, opts) do
      log? = Keyword.get(opts, :log, true)

      try do
        resolved_cmd =
          case System.find_executable(cmd) do
            nil -> raise ArgumentError, "executable not found: #{cmd}"
            path -> path
          end

        {output, status} =
          System.cmd(resolved_cmd, args, stderr_to_stdout: true)

        result = %{
          cmd: resolved_cmd,
          args: args,
          status: status,
          output: output
        }

        if log? do
          log_result(result)
        end

        {:ok, result}
      rescue
        exception in [ArgumentError, ErlangError] ->
          error(cmd, args, Exception.message(exception))
      end
    else
      {:error, reason} ->
        {:error, reason}
    end
  end

  def run(cmd, args, _opts) do
    error(cmd, args, "invalid arguments to run/3")
  end

  def start(cmd, args \\ [], opts \\ [])

  def start(cmd, args, opts) when is_binary(cmd) and is_list(args) and is_list(opts) do
    case Process.whereis(@task_supervisor) do
      nil ->
        Logger.error(
          "runner start failed cmd=#{inspect(cmd)} args=#{inspect(args)} reason=:task_supervisor_not_started"
        )

        {:error, :task_supervisor_not_started}

      _pid ->
        case Task.Supervisor.start_child(@task_supervisor, fn ->
               _ = run(cmd, args, opts)
               :ok
             end) do
          {:ok, _pid} ->
            :ok

          {:error, reason} ->
            Logger.error(
              "runner start failed cmd=#{inspect(cmd)} args=#{inspect(args)} reason=#{inspect(reason)}"
            )

            {:error, reason}
        end
    end
  end

  def start(cmd, args, _opts) do
    error(cmd, args, "invalid arguments to start/3")
  end

  defp validate(cmd, args, opts) do
    cond do
      not Enum.all?(args, &is_binary/1) ->
        error(cmd, args, "args must be a list of binaries")

      not Keyword.keyword?(opts) ->
        error(cmd, args, "opts must be a keyword list")

      not is_boolean(Keyword.get(opts, :log, true)) ->
        error(cmd, args, ":log must be a boolean")

      true ->
        :ok
    end
  end

  defp error(cmd, args, message) do
    reason = %{
      cmd: cmd,
      args: args,
      exception: message
    }

    Logger.error(
      "runner exception cmd=#{inspect(cmd)} args=#{inspect(args)} exception=#{inspect(message)}"
    )

    {:error, reason}
  end

  defp log_result(%{cmd: cmd, args: args, status: status, output: output}) do
    trimmed = String.trim(output)

    level =
      case status do
        0 -> :debug
        _ -> :error
      end

    message =
      if trimmed == "" do
        "command=#{inspect(cmd)} args=#{inspect(args)} status=#{status}"
      else
        "command=#{inspect(cmd)} args=#{inspect(args)} status=#{status} output=#{trimmed}"
      end

    Logger.log(level, message)
  end
end

defmodule Osd do
  use GenServer
  require Logger

  @aosd_args [
    "--position",
    "0",
    "--x-offset",
    "32",
    "--y-offset",
    "32",
    "--padding",
    "16",
    "--transparency",
    "0",
    "--font",
    "monospace 24",
    "--back-color",
    "black",
    "--fore-color",
    "white",
    "--fade-in",
    "0",
    "--fade-full",
    "5000",
    "--fade-out",
    "0",
    "--lines",
    "1"
  ]

  @kill_attempts [
    {"-TERM", 20},
    {"-TERM", 40},
    {"-KILL", 0}
  ]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def show(message) when is_binary(message) do
    GenServer.cast(__MODULE__, {:show, message})
  end

  def clear do
    GenServer.cast(__MODULE__, :clear)
  end

  @impl true
  def init(_opts) do
    {:ok, %{instances: []}}
  end

  @impl true
  def handle_cast({:show, ""}, state) do
    Logger.warning(
      "osd handle_cast show_empty " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.warning(
      "osd handle_cast show_empty done " <>
        "remaining_instances=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_cast({:show, message}, state) do
    Logger.warning(
      "osd handle_cast show " <>
        "message=#{inspect(message)} " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.warning(
      "osd handle_cast show after_kill_all " <>
        "message=#{inspect(message)} " <>
        "survivor_count=#{length(instances)}"
    )

    case start_instance(message) do
      {:ok, instance} ->
        Logger.warning(
          "osd start os_pid=#{inspect(instance.os_pid)} pgid=#{inspect(instance.pgid)} message=#{inspect(message)}"
        )

        new_instances = [instance | instances]

        Logger.warning(
          "osd handle_cast show done " <>
            "message=#{inspect(message)} " <>
            "instances_count=#{length(new_instances)}"
        )

        {:noreply, %{state | instances: new_instances}}

      {:error, reason} ->
        Logger.error(
          "osd start failed " <>
            "message=#{inspect(message)} " <>
            "reason=#{inspect(reason)}"
        )

        {:noreply, %{state | instances: instances}}
    end
  end

  @impl true
  def handle_cast(:clear, state) do
    Logger.warning(
      "osd handle_cast clear " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.warning(
      "osd handle_cast clear done " <>
        "remaining_instances=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info({port, {:exit_status, status}}, state) do
    Logger.warning(
      "osd port exit " <>
        "port=#{inspect(port)} " <>
        "status=#{inspect(status)} " <>
        "instances_before=#{length(state.instances)}"
    )

    instances =
      Enum.reject(state.instances, fn instance ->
        instance.port == port
      end)

    Logger.warning(
      "osd port exit pruned " <>
        "port=#{inspect(port)} " <>
        "instances_after=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info({:EXIT, port, reason}, state) when is_port(port) do
    Logger.warning(
      "osd port linked exit " <>
        "port=#{inspect(port)} " <>
        "reason=#{inspect(reason)} " <>
        "instances_before=#{length(state.instances)}"
    )

    instances =
      Enum.reject(state.instances, fn instance ->
        instance.port == port
      end)

    Logger.warning(
      "osd port linked exit pruned " <>
        "port=#{inspect(port)} " <>
        "instances_after=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info(msg, state) do
    Logger.warning("osd handle_info other msg=#{inspect(msg)}")
    {:noreply, state}
  end

  defp start_instance(message) do
    case System.find_executable("aosd_cat") do
      nil ->
        Logger.error("osd start_instance aosd_cat_not_found")
        {:error, :aosd_cat_not_found}

      aosd_cat ->
        Logger.warning(
          "osd start_instance opening_port " <>
            "executable=#{inspect(aosd_cat)} " <>
            "message=#{inspect(message)}"
        )

        port =
          Port.open({:spawn_executable, aosd_cat}, [
            :binary,
            :use_stdio,
            :hide,
            :exit_status,
            {:args, @aosd_args}
          ])

        os_pid =
          case Port.info(port, :os_pid) do
            {:os_pid, pid} when is_integer(pid) -> pid
            _ -> nil
          end

        pgid = get_pgid(os_pid)

        Logger.warning(
          "osd start_instance port_opened " <>
            "port=#{inspect(port)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)} " <>
            "message=#{inspect(message)}"
        )

        case Port.command(port, message <> "\n") do
          true ->
            Logger.warning(
              "osd start_instance command_sent " <>
                "port=#{inspect(port)} " <>
                "message=#{inspect(message)}"
            )

            {:ok, %{port: port, os_pid: os_pid, pgid: pgid, message: message}}

          false ->
            Logger.error(
              "osd start_instance command_failed " <>
                "port=#{inspect(port)} " <>
                "message=#{inspect(message)}"
            )

            safe_close_port(port)
            {:error, :port_command_failed}
        end
    end
  end

  defp kill_all(instances) do
    Logger.warning("osd kill_all begin instances_count=#{length(instances)}")

    survivors =
      instances
      |> Enum.reverse()
      |> Enum.reduce([], fn instance, acc ->
        if kill_instance(instance) do
          acc
        else
          [instance | acc]
        end
      end)

    Logger.warning(
      "osd kill_all done " <>
        "instances_before=#{length(instances)} " <>
        "survivor_count=#{length(survivors)}"
    )

    survivors
  end

  defp kill_instance(%{port: port, os_pid: os_pid, pgid: pgid, message: message} = _instance) do
    target =
      cond do
        is_integer(pgid) and pgid > 0 -> {-pgid, "pgid"}
        is_integer(os_pid) and os_pid > 0 -> {os_pid, "pid"}
        true -> {nil, "none"}
      end

    case target do
      {nil, _} ->
        Logger.warning(
          "osd kill no_target " <>
            "message=#{inspect(message)} " <>
            "port=#{inspect(port)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        safe_close_port(port)
        true

      {kill_target, target_kind} ->
        Logger.warning(
          "osd kill begin " <>
            "message=#{inspect(message)} " <>
            "target_kind=#{target_kind} " <>
            "target=#{inspect(kill_target)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        Enum.each(@kill_attempts, fn {signal, sleep_ms} ->
          Logger.warning(
            "osd kill attempt " <>
              "message=#{inspect(message)} " <>
              "signal=#{inspect(signal)} " <>
              "target=#{inspect(kill_target)} " <>
              "sleep_ms=#{inspect(sleep_ms)}"
          )

          _ = System.cmd("kill", [signal, Integer.to_string(kill_target)], stderr_to_stdout: true)

          if sleep_ms > 0 do
            Process.sleep(sleep_ms)
          end
        end)

        dead? =
          cond do
            is_integer(pgid) and pgid > 0 ->
              not alive_by_group?(pgid)

            is_integer(os_pid) and os_pid > 0 ->
              not alive_by_pid?(os_pid)

            true ->
              true
          end

        safe_close_port(port)

        Logger.warning(
          "osd kill result " <>
            "message=#{inspect(message)} " <>
            "dead?=#{inspect(dead?)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        dead?
    end
  end

  defp alive_by_pid?(pid) when is_integer(pid) and pid > 0 do
    case System.cmd("kill", ["-0", Integer.to_string(pid)], stderr_to_stdout: true) do
      {_out, 0} -> true
      {_out, _status} -> false
    end
  end

  defp alive_by_pid?(_), do: false

  defp alive_by_group?(pgid) when is_integer(pgid) and pgid > 0 do
    case System.cmd("kill", ["-0", "--", Integer.to_string(-pgid)], stderr_to_stdout: true) do
      {_out, 0} -> true
      {_out, _status} -> false
    end
  end

  defp alive_by_group?(_), do: false

  defp get_pgid(pid) when is_integer(pid) and pid > 0 do
    case System.cmd("ps", ["-o", "pgid=", "-p", Integer.to_string(pid)], stderr_to_stdout: true) do
      {output, 0} ->
        output
        |> String.trim()
        |> case do
          "" ->
            nil

          value ->
            case Integer.parse(value) do
              {pgid, ""} -> pgid
              _ -> nil
            end
        end

      _ ->
        nil
    end
  end

  defp get_pgid(_), do: nil

  defp safe_close_port(port) when is_port(port) do
    try do
      Port.close(port)
    rescue
      _ -> :ok
    catch
      _, _ -> :ok
    end
  end

  defp safe_close_port(_), do: :ok
end

defmodule Volume do
  @cmd Path.expand("volume.sh", __DIR__)

  def toggle_mute, do: Runner.start(@cmd, [])
  def increase, do: Runner.start(@cmd, ["+"])
  def decrease, do: Runner.start(@cmd, ["-"])
end

defmodule Power do
  @cmd Path.expand("power.sh", __DIR__)

  def toggle, do: Runner.start(@cmd, [])
end

defmodule Channel do
  @cmd Path.expand("channel.sh", __DIR__)

  def next, do: Runner.start(@cmd, ["+"])
  def prev, do: Runner.start(@cmd, ["-"])
end

defmodule App.Util do
  require Logger

  def wake_keypress?(events) do
    Enum.any?(events, fn
      {:ev_key, _key, 1} -> true
      _ -> false
    end)
  end

  def current_original_path(%{current_channel: nil}), do: nil
  def current_original_path(%{current_file_index: idx}) when not is_integer(idx), do: nil

  def current_original_path(state),
    do: Enum.at(state.current_channel.media, state.current_file_index)

  def channel_id(channel), do: channel.path

  def normalize_index(value, media) when is_integer(value) do
    count = length(media)

    cond do
      count <= 0 -> 0
      value >= 0 -> rem(value, count)
      true -> 0
    end
  end

  def normalize_index(_value, _media), do: 0

  def next_file_index(current_index, media) when is_list(media) and media != [] do
    rem(normalize_index(current_index, media) + 1, length(media))
  end

  def next_file_index(_current_index, _media), do: 0

  def normalize_offset(value) when is_integer(value) and value >= 0, do: value
  def normalize_offset(value) when is_float(value) and value >= 0, do: trunc(value)
  def normalize_offset(_), do: 0

  def current_channel_index(channels, channel) do
    Enum.find_index(channels, &same_channel?(&1, channel))
  end

  def preview_base_channel(channels, preview_channel, current_channel) do
    preview_channel || current_channel || List.first(channels)
  end

  def preview_base_index(channels, preview_channel, current_channel) do
    base_channel = preview_base_channel(channels, preview_channel, current_channel)

    case current_channel_index(channels, base_channel) do
      nil ->
        Logger.warning(
          "preview_base_index: base channel not found, falling back to index 0 " <>
            "base_channel=#{inspect(base_channel && channel_id(base_channel))} " <>
            "channels=#{inspect(Enum.map(channels, &channel_id/1))}"
        )

        0

      index ->
        index
    end
  end

  def same_channel?(nil, nil), do: true
  def same_channel?(nil, _), do: false
  def same_channel?(_, nil), do: false
  def same_channel?(a, b), do: channel_id(a) == channel_id(b)

  def cancel_preview_timer(nil), do: :ok
  def cancel_preview_timer(timer_ref), do: Process.cancel_timer(timer_ref)

  def cancel_image_display_timer(nil), do: :ok

  def cancel_image_display_timer(timer_ref) do
    result = Process.cancel_timer(timer_ref)

    Logger.debug(
      "cancel_image_display_timer: timer_ref=#{inspect(timer_ref)} cancel_result=#{inspect(result)}"
    )

    result
  end
end

defmodule App.Persistence do
  require Logger

  @resume_rewind_seconds 3

  def persist_current_channel_position(state), do: persist_state_file!(state)

  def put_current_playhead(%{current_channel: nil} = state, _offset_seconds), do: state
  def put_current_playhead(%{current_channel_id: nil} = state, _offset_seconds), do: state

  def put_current_playhead(%{current_file_index: idx} = state, _offset_seconds)
      when not is_integer(idx),
      do: state

  def put_current_playhead(state, offset_seconds) do
    current_path = App.Util.current_original_path(state)
    normalized_offset = normalize_playhead_offset(current_path, offset_seconds)

    Logger.debug(
      "put_current_playhead: channel_id=#{inspect(state.current_channel_id)} file_index=#{inspect(state.current_file_index)} path=#{inspect(current_path)} offset_seconds=#{normalized_offset}"
    )

    %{
      state
      | channel_state:
          put_channel_position(
            state.channel_state,
            state.current_channel_id,
            current_path,
            normalized_offset
          )
    }
  end

  def ensure_all_channel_state_entries(state) do
    channel_state =
      Enum.reduce(state.channels, state.channel_state, fn channel, acc ->
        Map.put_new(acc, App.Util.channel_id(channel), empty_channel_position())
      end)

    %{state | channel_state: channel_state}
  end

  def load_or_initialize_channel_position(state, channel) do
    id = App.Util.channel_id(channel)

    case Map.fetch(state.channel_state, id) do
      {:ok, %{"file_path" => saved_file_path, "offset_seconds" => offset_seconds}} ->
        file_index = resolve_file_index(channel.media, saved_file_path)
        restored_offset = restore_saved_offset_for_channel(channel, file_index, offset_seconds)
        {file_index, restored_offset, state}

      :error ->
        updated_channel_state = Map.put(state.channel_state, id, empty_channel_position())
        {0, 0, %{state | channel_state: updated_channel_state}}
    end
  end

  def persisted_resume_offset(offset_seconds),
    do: max(App.Util.normalize_offset(offset_seconds) - @resume_rewind_seconds, 0)

  def persist_state_file!(state) do
    persisted = %{
      "version" => 1,
      "current_channel_id" => state.current_channel_id,
      "channel_state" => state.channel_state
    }

    case StateStore.save!(persisted) do
      :ok ->
        state

      :error ->
        # Do not crash playback; log and keep going
        Logger.error("persist_state_file!: failed to save state.json; continuing without crash")
        state
    end
  end

  defp restore_saved_offset_for_channel(channel, file_index, saved_offset_seconds) do
    path = Enum.at(channel.media, file_index)
    normalize_playhead_offset(path, saved_offset_seconds)
  end

  defp resolve_file_index(media, saved_file_path) when is_binary(saved_file_path),
    do: Enum.find_index(media, &(&1 == saved_file_path)) || 0

  defp resolve_file_index(_media, _saved_file_path), do: 0

  defp normalize_playhead_offset(path, offset_seconds) do
    if PreparedMedia.image_file?(path) do
      0
    else
      App.Util.normalize_offset(offset_seconds)
    end
  end

  defp put_channel_position(channel_state, channel_id, path, offset_seconds) do
    Map.put(channel_state, channel_id, %{
      "file_path" => path,
      "offset_seconds" => offset_seconds
    })
  end

  defp empty_channel_position do
    %{
      "file_path" => nil,
      "offset_seconds" => 0
    }
  end
end

defmodule App.Playback do
  require Logger

  def pause_playback(state) do
    Logger.info(
      "pause_playback: " <>
        "current_channel=#{inspect(state.current_channel && state.current_channel.label)} " <>
        "current_file_index=#{inspect(state.current_file_index)} " <>
        "current_player=#{inspect(state.current_player)} " <>
        "next_prepare_ref=#{inspect(state.next_prepare_ref)} " <>
        "next_prepared?=#{inspect(not is_nil(state.next_prepared))}"
    )

    state
    |> App.Advance.clear_transition_state()
    |> App.Persistence.persist_current_channel_position()
    |> stop_current_item()
  end

  def resume_playback(state) do
    Logger.info(
      "resume_playback: " <>
        "current_channel=#{inspect(state.current_channel && state.current_channel.label)} " <>
        "saved_current_channel_id=#{inspect(state.current_channel_id)}"
    )

    state = App.Advance.clear_transition_state(state)

    case state.current_channel do
      nil ->
        Logger.debug("resume_playback: no current channel to resume")
        state

      channel ->
        {file_index, offset_seconds, state} =
          App.Persistence.load_or_initialize_channel_position(state, channel)

        Logger.info(
          "resume_playback: resuming channel=#{channel.label} " <>
            "requested_file_index=#{inspect(file_index)} " <>
            "offset_seconds=#{inspect(offset_seconds)}"
        )

        case play_channel_item(state, channel, file_index, offset_seconds) do
          {:ok, state, actual_file_index} ->
            actual_path = Enum.at(channel.media, actual_file_index)

            Logger.info(
              "resume_playback: resumed channel=#{channel.label} " <>
                "actual_file_index=#{actual_file_index} " <>
                "actual_path=#{inspect(actual_path)} " <>
                "offset_seconds=#{inspect(offset_seconds)}"
            )

            state

          {:error, state, reason} ->
            Logger.error("resume_playback: failed to resume #{inspect(reason)}")
            state
        end
    end
  end

  def play_channel_item(state, channel, file_index, offset_seconds) do
    Logger.debug(
      "play_channel_item: channel=#{channel.label} requested_index=#{inspect(file_index)} offset_seconds=#{inspect(offset_seconds)} root=#{inspect(channel.root)} media_count=#{length(channel.media || [])}"
    )

    case File.ls(channel.root) do
      {:ok, _entries} ->
        media = channel.media || []

        cond do
          media == [] ->
            Logger.error("play_channel_item: empty channel channel=#{channel.label}")
            {:error, state, {:channel_unavailable, :empty_channel}}

          true ->
            normalized_index = App.Util.normalize_index(file_index, media)

            Logger.debug(
              "play_channel_item: channel available channel=#{channel.label} normalized_index=#{normalized_index}"
            )

            try_play_channel_item(
              state,
              channel,
              media,
              normalized_index,
              offset_seconds,
              normalized_index,
              0,
              length(media)
            )
        end

      {:error, reason} ->
        Logger.error(
          "play_channel_item: channel unavailable channel=#{channel.label} root=#{inspect(channel.root)} reason=#{inspect(reason)}"
        )

        {:error, state, {:channel_unavailable, reason}}
    end
  end

  def start_current_item(state, channel, file_index, prepared, startpos) do
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "start_current_item: channel=#{channel.label} index=#{file_index} actual_path=#{inspect(actual_path)} play_path=#{inspect(prepared.play_path)} startpos=#{inspect(startpos)} tmp?=#{inspect(prepared.tmp?)} downscaled?=#{inspect(prepared.downscaled?)}"
    )

    with {:ok, playerpid} <-
           MpvPlayer.start_link(parent: self(), file: prepared.play_path, start_pos: startpos) do
      new_state =
        build_started_playback_state(state, channel, file_index, playerpid, actual_path)

      Logger.debug(
        "start_current_item: started player pid=#{inspect(playerpid)} channel=#{channel.label} index=#{file_index}"
      )

      {:ok, new_state, file_index}
    else
      {:error, reason} = error ->
        Logger.error(
          "start_current_item: failed channel=#{channel.label} index=#{file_index} actual_path=#{inspect(actual_path)} play_path=#{inspect(prepared.play_path)} reason=#{inspect(reason)}"
        )

        error
    end
  end

  def stop_current_item(%{current_player: nil} = state) do
    Logger.debug(
      "stop_current_item: no current player current_path=#{inspect(App.Util.current_original_path(state))}"
    )

    state
  end

  def stop_current_item(%{current_player: player} = state) do
    Logger.info(
      "stop_current_item: player=#{inspect(player)} current_path=#{inspect(App.Util.current_original_path(state))}"
    )

    result =
      try do
        MpvPlayer.stop(player)
      catch
        :exit, {:noproc, _} = reason ->
          Logger.warning(
            "stop_current_item: player already gone player=#{inspect(player)} reason=#{inspect(reason)}"
          )

          :ok

        :exit, reason ->
          Logger.warning(
            "stop_current_item: stop raised exit player=#{inspect(player)} reason=#{inspect(reason)}"
          )

          {:error, reason}
      end

    case result do
      :ok ->
        Logger.debug("stop_current_item: player fully stopped player=#{inspect(player)}")

      {:ok, :already_stopping} ->
        Logger.debug("stop_current_item: player already stopping player=#{inspect(player)}")

      {:error, reason} ->
        Logger.warning(
          "stop_current_item: stop returned error player=#{inspect(player)} reason=#{inspect(reason)}"
        )

      other ->
        Logger.warning(
          "stop_current_item: unexpected stop result player=#{inspect(player)} result=#{inspect(other)}"
        )
    end

    %{state | current_player: nil}
  end

  defp build_started_playback_state(state, channel, file_index, playerpid, actual_path) do
    state
    |> App.Advance.clear_transition_state()
    |> Map.put(:current_channel, channel)
    |> Map.put(:current_channel_id, App.Util.channel_id(channel))
    |> Map.put(:current_file_index, file_index)
    |> Map.put(:current_player, playerpid)
    |> App.Persistence.put_current_playhead(0)
    |> App.Persistence.persist_state_file!()
    |> App.Advance.maybe_start_image_display_timer(actual_path)
    |> App.Advance.maybe_prepare_next_item()
  end

  defp try_play_channel_item(
         state,
         _channel,
         _media,
         _requested_index,
         _offset_seconds,
         _candidate_index,
         attempts,
         max_attempts
       )
       when attempts >= max_attempts do
    {:error, state, {:channel_unavailable, :no_playable_files}}
  end

  defp try_play_channel_item(
         state,
         channel,
         media,
         requested_index,
         offset_seconds,
         candidate_index,
         attempts,
         max_attempts
       ) do
    path = Enum.at(media, candidate_index)

    Logger.debug(
      "try_play_channel_item: channel=#{channel.label} requested_index=#{inspect(requested_index)} candidate_index=#{inspect(candidate_index)} attempts=#{attempts}/#{max_attempts} path=#{inspect(path)}"
    )

    cond do
      not is_binary(path) ->
        Logger.warning(
          "try_play_channel_item: skipping non-binary path channel=#{channel.label} candidate_index=#{inspect(candidate_index)} value=#{inspect(path)}"
        )

        try_next_candidate(
          state,
          channel,
          media,
          requested_index,
          offset_seconds,
          candidate_index,
          attempts,
          max_attempts
        )

      true ->
        case PreparedMedia.prepare_for_playback(path) do
          {:ok, prepared} ->
            startpos =
              if PreparedMedia.image_file?(path),
                do: 0,
                else: App.Util.normalize_offset(offset_seconds)

            case start_current_item(state, channel, candidate_index, prepared, startpos) do
              {:ok, state, actual_file_index} ->
                {:ok, state, actual_file_index}

              {:error, _reason} ->
                try_next_candidate(
                  state,
                  channel,
                  media,
                  requested_index,
                  offset_seconds,
                  candidate_index,
                  attempts,
                  max_attempts
                )
            end

          {:error, reason} ->
            Logger.error(
              "try_play_channel_item: prepare failed channel=#{channel.label} candidate_index=#{inspect(candidate_index)} path=#{inspect(path)} reason=#{inspect(reason)}"
            )

            try_next_candidate(
              state,
              channel,
              media,
              requested_index,
              offset_seconds,
              candidate_index,
              attempts,
              max_attempts
            )
        end
    end
  end

  defp try_next_candidate(
         state,
         channel,
         media,
         requested_index,
         offset_seconds,
         candidate_index,
         attempts,
         max_attempts
       ) do
    next_candidate_index = rem(candidate_index + 1, length(media))

    try_play_channel_item(
      state,
      channel,
      media,
      requested_index,
      offset_seconds,
      next_candidate_index,
      attempts + 1,
      max_attempts
    )
  end
end

defmodule App.Preview do
  @moduledoc false

  @preview_commit_ms 500

  def cancel_active_preview(%{preview_active?: false} = state), do: state

  def cancel_active_preview(state) do
    clear_preview_state(state)
  end

  def preview_next_channel(%{channels: []} = state), do: state

  def preview_next_channel(%{channels: channels} = state) do
    base_channel =
      Map.get(state, :preview_channel) ||
        Map.get(state, :current_channel) ||
        List.first(channels)

    current_index = App.Util.current_channel_index(channels, base_channel)
    next_index = rem(current_index + 1, length(channels))
    begin_preview(state, Enum.at(channels, next_index))
  end

  def preview_prev_channel(%{channels: []} = state), do: state

  def preview_prev_channel(%{channels: channels} = state) do
    base_channel =
      Map.get(state, :preview_channel) ||
        Map.get(state, :current_channel) ||
        List.first(channels)

    current_index = App.Util.current_channel_index(channels, base_channel)
    prev_index = rem(current_index - 1 + length(channels), length(channels))
    begin_preview(state, Enum.at(channels, prev_index))
  end

  def commit_preview(%{preview_channel: nil} = state) do
    clear_preview_state(state)
  end

  def commit_preview(%{switching?: true} = state) do
    state
  end

  def commit_preview(state) do
    requested_channel = state.preview_channel

    if App.Util.same_channel?(requested_channel, state.current_channel) do
      clear_preview_state(state)
    else
      seq = state.request_seq + 1
      owner = self()

      Task.start(fn ->
        do_channel_switch(owner, seq, state, requested_channel)
      end)

      state
      |> Map.put(:request_seq, seq)
      |> Map.put(:switching?, true)
      |> Map.put(:switching_seq, seq)
    end
  end

  def handle_switch_result(%{switching_seq: seq, request_seq: seq} = _state, {:ok, new_state}) do
    new_state
    |> Map.put(:switching?, false)
    |> Map.put(:switching_seq, nil)
    |> clear_preview_state()
  end

  def handle_switch_result(
        %{switching_seq: seq, request_seq: current_seq} = state,
        {:ok, new_state}
      )
      when seq != current_seq do
    new_state
    |> Map.put(:preview_channel, state.preview_channel)
    |> Map.put(:preview_timer, state.preview_timer)
    |> Map.put(:preview_active?, state.preview_active?)
    |> Map.put(:commit_seq, state.commit_seq)
    |> Map.put(:request_seq, state.request_seq)
    |> Map.put(:switching?, false)
    |> Map.put(:switching_seq, nil)
    |> maybe_schedule_followup_commit()
  end

  def handle_switch_result(state, {:error, reason}) do
    require Logger
    Logger.error("handle_switch_result channel switch failed #{inspect(reason)}")

    state
    |> Map.put(:switching?, false)
    |> Map.put(:switching_seq, nil)
    |> maybe_schedule_followup_commit()
  end

  def clear_preview_state(state) do
    App.Util.cancel_preview_timer(Map.get(state, :preview_timer))

    state
    |> Map.put(:preview_channel, nil)
    |> Map.put(:preview_timer, nil)
    |> Map.put(:preview_active?, false)
  end

  defp begin_preview(state, channel) do
    App.Util.cancel_preview_timer(Map.get(state, :preview_timer))

    commit_seq = Map.get(state, :commit_seq, 0) + 1
    timer_ref = Process.send_after(self(), {:commit_preview, commit_seq}, @preview_commit_ms)

    if not App.Util.same_channel?(channel, Map.get(state, :current_channel)) do
      Osd.show(channel.name)
    end

    state
    |> Map.put(:preview_channel, channel)
    |> Map.put(:preview_timer, timer_ref)
    |> Map.put(:preview_active?, true)
    |> Map.put(:commit_seq, commit_seq)
  end

  defp maybe_schedule_followup_commit(state) do
    cond do
      Map.get(state, :preview_channel) == nil ->
        state

      App.Util.same_channel?(Map.get(state, :preview_channel), Map.get(state, :current_channel)) ->
        clear_preview_state(state)

      true ->
        Process.send_after(self(), {:commit_preview, Map.get(state, :commit_seq, 0)}, 0)
        state
    end
  end

  defp do_channel_switch(owner, seq, state, requested_channel) do
    try do
      state =
        state
        |> App.Advance.clear_transition_state()
        |> App.Persistence.persist_current_channel_position()

      state = App.Playback.stop_current_item(state)

      {file_index, offset_seconds, state} =
        App.Persistence.load_or_initialize_channel_position(state, requested_channel)

      case App.Playback.play_channel_item(state, requested_channel, file_index, offset_seconds) do
        {:ok, new_state, _actual_file_index} ->
          new_state =
            new_state
            |> Map.put(:current_channel, requested_channel)
            |> Map.put(:current_channel_id, App.Util.channel_id(requested_channel))

          send(owner, {:channel_switch_result, seq, {:ok, new_state}})

        {:error, new_state, reason} ->
          send(owner, {:channel_switch_result, seq, {:error, reason, new_state}})
      end
    catch
      kind, reason ->
        send(owner, {:channel_switch_result, seq, {:error, {kind, reason}}})
    end
  end
end

defmodule App.Advance do
  require Logger

  @slideshow_image_display_ms 10_000

  def maybe_advance(%{current_channel: nil} = state) do
    Logger.debug("maybe_advance no current channel")
    state
  end

  def maybe_advance(state) do
    current_path = App.Util.current_original_path(state)
    image? = PreparedMedia.image_file?(current_path)

    Logger.debug(
      "maybe_advance current_path=#{inspect(current_path)} image?=#{inspect(image?)} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared=#{inspect(Map.take(state.next_prepared || %{}, [:file_index, :path, :result]))}"
    )

    cond do
      image? and state.display_elapsed? and not is_nil(state.next_prepared) ->
        Logger.info("maybe_advance conditions met, advancing to prepared next item")
        advance_to_prepared_next(state)

      true ->
        Logger.debug("maybe_advance conditions not met, waiting")
        state
    end
  end

  def advance_to_prepared_next(
        %{next_prepared: %{file_index: file_index, result: {:ok, prepared}}} = state
      ) do
    channel = state.current_channel
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "advance_to_prepared_next channel=#{channel.label} file_index=#{file_index} actual_path=#{inspect(actual_path)} prepared_playpath=#{inspect(prepared.play_path)}"
    )

    state =
      state
      |> clear_transition_state()
      |> App.Playback.stop_current_item()

    case App.Playback.start_current_item(state, channel, file_index, prepared, 0) do
      {:ok, state, _actual_file_index} ->
        Logger.info("advanced #{channel.label} to prepared #{inspect(actual_path)}")
        state

      {:error, reason} ->
        Logger.error(
          "failed starting prepared next item #{inspect(actual_path)} #{inspect(reason)}"
        )

        advance_current_channel_item(state)
    end
  end

  def advance_to_prepared_next(%{next_prepared: %{path: path, result: {:error, reason}}} = state) do
    Logger.error("async prepare failed for next item #{inspect(path)} #{inspect(reason)}")

    state
    |> clear_transition_state()
    |> advance_current_channel_item()
  end

  def advance_to_prepared_next(state) do
    Logger.debug(
      "advance_to_prepared_next no usable prepared item next_prepared=#{inspect(state.next_prepared)}"
    )

    state
  end

  def advance_current_channel_item(%{current_channel: nil} = state) do
    Logger.debug("advance_current_channel_item skip no current channel")
    state
  end

  def advance_current_channel_item(%{switching?: true} = state) do
    Logger.info(
      "advance_current_channel_item skipping because channel switch is in progress current_channel=#{inspect(state.current_channel && state.current_channel.label)} current_index=#{inspect(state.current_file_index)} switching_seq=#{inspect(state.switching_seq)}"
    )

    %{state | current_player: nil}
  end

  def advance_current_channel_item(state) do
    channel = state.current_channel
    requested_file_index = App.Util.next_file_index(state.current_file_index, channel.media)

    Logger.info(
      "advance_current_channel_item channel=#{channel.label} current_index=#{inspect(state.current_file_index)} requested_next_index=#{requested_file_index} current_player=#{inspect(state.current_player)}"
    )

    state = App.Playback.stop_current_item(state)

    case App.Playback.play_channel_item(state, channel, requested_file_index, 0) do
      {:ok, state, actual_file_index} ->
        actual_path = Enum.at(channel.media, actual_file_index)

        Logger.info(
          "advance_current_channel_item started #{channel.label} actual_index=#{actual_file_index} actual_path=#{inspect(actual_path)}"
        )

        state
        |> App.Persistence.put_current_playhead(0)
        |> App.Persistence.persist_state_file!()

      {:error, state, reason} ->
        Logger.error(
          "advance_current_channel_item failed channel=#{channel.label} requested_next_index=#{requested_file_index} reason=#{inspect(reason)}"
        )

        %{state | current_player: nil}
    end
  end

  def maybe_start_image_display_timer(state, path) do
    App.Util.cancel_image_display_timer(state.image_display_timer)

    if PreparedMedia.image_file?(path) do
      timer_ref = Process.send_after(self(), :image_display_expired, @slideshow_image_display_ms)

      Logger.debug(
        "maybe_start_image_display_timer armed path=#{inspect(path)} timer_ref=#{inspect(timer_ref)} display_ms=#{@slideshow_image_display_ms}"
      )

      %{state | image_display_timer: timer_ref, display_elapsed?: false}
    else
      Logger.debug("maybe_start_image_display_timer skipped non-image path=#{inspect(path)}")
      %{state | image_display_timer: nil, display_elapsed?: false}
    end
  end

  def maybe_prepare_next_item(%{current_channel: nil} = state) do
    Logger.debug("maybe_prepare_next_item skip no current channel")
    state
  end

  def maybe_prepare_next_item(%{next_prepare_task: task} = state) when not is_nil(task) do
    Logger.debug("maybe_prepare_next_item skip task already running")
    state
  end

  def maybe_prepare_next_item(%{next_prepared: prepared} = state) when not is_nil(prepared) do
    Logger.debug("maybe_prepare_next_item skip next item already prepared")
    state
  end

  def maybe_prepare_next_item(state) do
    media = state.current_channel.media

    if media == [] or not is_integer(state.current_file_index) do
      Logger.debug(
        "maybe_prepare_next_item skip media_empty?=#{inspect(media == [])} current_file_index=#{inspect(state.current_file_index)}"
      )

      state
    else
      next_index = App.Util.next_file_index(state.current_file_index, media)
      next_path = Enum.at(media, next_index)

      Logger.debug(
        "maybe_prepare_next_item starting async prepare next_index=#{next_index} next_path=#{inspect(next_path)} current_index=#{inspect(state.current_file_index)} current_path=#{inspect(App.Util.current_original_path(state))}"
      )

      start_prepare_next_task(state, next_index, next_path)
    end
  end

  def clear_transition_state(state) do
    Logger.debug(
      "clear_transition_state clearing image timer and prepared-next state current_path=#{inspect(App.Util.current_original_path(state))}"
    )

    state
    |> cancel_image_timer_and_reset_gate()
    |> clear_next_prepare()
  end

  defp cancel_image_timer_and_reset_gate(state) do
    App.Util.cancel_image_display_timer(state.image_display_timer)

    state
    |> Map.put(:image_display_timer, nil)
    |> Map.put(:display_elapsed?, false)
  end

  def clear_next_prepare(%{next_prepare_task: nil} = state), do: state

  def clear_next_prepare(state) do
    state
    |> Map.put(:next_prepare_task, nil)
    |> Map.put(:next_prepare_ref, nil)
    |> Map.put(:next_prepared, nil)
    |> Map.put(:next_requested_file_index, nil)
  end

  defp start_prepare_next_task(state, next_index, next_path) do
    owner = self()
    ref = make_ref()

    task =
      Task.async(fn ->
        started_ms = System.monotonic_time(:millisecond)
        result = PreparedMedia.prepare_for_playback(next_path)
        elapsed_ms = System.monotonic_time(:millisecond) - started_ms

        Logger.info(
          "maybe_prepare_next_item finished async prepare next_index=#{next_index} next_path=#{inspect(next_path)} elapsed_ms=#{elapsed_ms} result=#{inspect(result)}"
        )

        send(owner, {ref, {:prepared_next_item, next_index, next_path, result}})
      end)

    state
    |> Map.put(:next_prepare_task, task)
    |> Map.put(:next_prepare_ref, ref)
    |> Map.put(:next_requested_file_index, next_index)
  end
end

defmodule App.Input do
  use GenServer
  require Logger

  @preview_commit_ms 2000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, opts)
  end

  def input_event(server, dev, events) do
    send(server, {:input_event, dev, events})
    :ok
  end

  defmodule PreviewSession do
    defstruct seq: 0,
              base_channel: nil,
              target_channel: nil,
              timer_ref: nil,
              wrapped_to_current?: false,
              switching?: false
  end

  defmodule State do
    defstruct channels: [],
              playback_owner: nil,
              current_channel: nil,
              preview_session: nil,
              next_seq: 0,
              pressed_keys: MapSet.new()
  end

  @impl true
  def init(opts) do
    state = %State{
      channels: Keyword.fetch!(opts, :channels),
      playback_owner: Keyword.get(opts, :playback_owner),
      current_channel: Keyword.get(opts, :current_channel)
    }

    {:ok, state}
  end

  @impl true
  def handle_info({:input_event, dev, events}, state) when is_list(events) do
    Logger.warning("input_event dev=#{inspect(dev)} events=#{inspect(events)}")

    state =
      Enum.reduce(events, state, fn
        {:ev_key, :key_pageup, 1}, state ->
          Logger.warning("PgUp from dev=#{inspect(dev)}")
          _ = Channel.prev()
          preview_prev_channel(state)

        {:ev_key, :key_pagedown, 1}, state ->
          Logger.warning("PgDn from dev=#{inspect(dev)}")
          _ = Channel.next()
          preview_next_channel(state)

        {:ev_key, :key_space, 1}, state ->
          Logger.warning("Space from dev=#{inspect(dev)}")
          _ = Power.toggle()

        {:ev_key, :key_q, 1}, state ->
          Logger.warning("Q from dev=#{inspect(dev)}")

          if state.playback_owner do
            GenServer.cast(state.playback_owner, :quit)
          end

          Process.send_after(self(), :quit, 0)
          state

        {:ev_key, :key_m, 1}, state ->
          Logger.warning("M from dev=#{inspect(dev)}")
          _ = Volume.toggle_mute()
          state

        {:ev_key, :key_minus, 1}, state ->
          Logger.warning("- from dev=#{inspect(dev)}")
          _ = Volume.decrease()
          state

        {:ev_key, :key_kpminus, 1}, state ->
          Logger.warning("Numpad - from dev=#{inspect(dev)}")
          _ = Volume.decrease()
          state

        {:ev_key, :key_equal, 1}, state ->
          Logger.warning("= from dev=#{inspect(dev)}")
          _ = Volume.increase()
          state

        {:ev_key, :key_plus, 1}, state ->
          Logger.warning("+ from dev=#{inspect(dev)}")
          _ = Volume.increase()
          state

        {:ev_key, :key_kpplus, 1}, state ->
          Logger.warning("Numpad + from dev=#{inspect(dev)}")
          _ = Volume.increase()
          state

        _, state ->
          state
      end)

    {:noreply, state}
  end

  @impl true
  def handle_info(
        {:commit_preview, seq},
        %{preview_session: %PreviewSession{seq: active_seq}} = state
      )
      when seq != active_seq do
    Logger.warning(
      "commit_preview ignored: stale_timer " <>
        "timer_seq=#{inspect(seq)} " <>
        "active_seq=#{inspect(active_seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "preview=#{inspect(state.preview_session && state.preview_session.target_channel && state.preview_session.target_channel.name)}"
    )

    {:noreply, state}
  end

  @impl true
  def handle_info({:commit_preview, _seq}, %{preview_session: nil} = state) do
    Logger.warning(
      "commit_preview ignored: no_preview_session " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)}"
    )

    {:noreply, state}
  end

  @impl true
  def handle_info(
        {:commit_preview, seq},
        %{preview_session: %PreviewSession{seq: seq, switching?: true}} = state
      ) do
    Logger.warning(
      "commit_preview ignored: already_switching " <>
        "seq=#{inspect(seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "target=#{inspect(state.preview_session.target_channel && state.preview_session.target_channel.name)}"
    )

    {:noreply, state}
  end

  @impl true
  def handle_info({:commit_preview, seq}, %{playback_owner: nil} = state) do
    Logger.warning(
      "commit_preview ignored: no_playback_owner " <>
        "seq=#{inspect(seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)}"
    )

    {:noreply, clear_preview_session(state)}
  end

  @impl true
  def handle_info(
        {:commit_preview, seq},
        %{preview_session: %PreviewSession{seq: seq} = session} = state
      ) do
    Logger.warning(
      "commit_preview handling " <>
        "seq=#{inspect(seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "target=#{inspect(session.target_channel && session.target_channel.name)} " <>
        "wrapped_to_current?=#{inspect(session.wrapped_to_current?)}"
    )

    if App.Util.same_channel?(session.target_channel, state.current_channel) do
      Logger.warning(
        "commit_preview cleared: preview_equals_current " <>
          "channel=#{inspect(state.current_channel && state.current_channel.name)} " <>
          "seq=#{inspect(seq)}"
      )

      {:noreply, clear_preview_session(state)}
    else
      Logger.warning(
        "commit_preview begin_switch " <>
          "seq=#{inspect(seq)} " <>
          "from=#{inspect(state.current_channel && state.current_channel.name)} " <>
          "to=#{inspect(session.target_channel && session.target_channel.name)}"
      )

      GenServer.cast(state.playback_owner, {:begin_switch, seq, session.target_channel})

      {:noreply, put_in(state.preview_session.switching?, true)}
    end
  end

  @impl true
  def handle_info(
        {:switch_result, seq, :ok, channel},
        %{preview_session: %PreviewSession{seq: seq}} = state
      ) do
    Logger.warning(
      "switch_result success " <>
        "seq=#{inspect(seq)} " <>
        "channel=#{inspect(channel && channel.name)}"
    )

    state =
      state
      |> Map.put(:current_channel, channel)
      |> clear_preview_session()

    {:noreply, state}
  end

  @impl true
  def handle_info(
        {:switch_result, seq, {:error, reason}, channel},
        %{preview_session: %PreviewSession{seq: seq}} = state
      ) do
    Logger.error(
      "channel switch failed " <>
        "seq=#{inspect(seq)} " <>
        "channel=#{inspect(channel && channel.name)} " <>
        "reason=#{inspect(reason)}"
    )

    state =
      case state.preview_session do
        nil -> state
        session -> %{state | preview_session: %{session | switching?: false}}
      end

    {:noreply, state}
  end

  @impl true
  def handle_info({:switch_result, _seq, _result, _channel}, state) do
    {:noreply, state}
  end

  @impl true
  def handle_info({:playback_started, channel}, state) do
    {:noreply, %{state | current_channel: channel}}
  end

  @impl true
  def handle_info({:playback_failed, _channel, _reason}, state) do
    {:noreply, state}
  end

  @impl true
  def handle_info(:quit, state) do
    Osd.clear()
    {:stop, :normal, state}
  end

  @impl true
  def handle_info(_msg, state), do: {:noreply, state}

  defp preview_next_channel(%State{channels: []} = state), do: state

  defp preview_next_channel(%State{channels: channels} = state) do
    base_channel =
      case state.preview_session do
        %PreviewSession{target_channel: channel} when not is_nil(channel) ->
          channel

        _ ->
          state.current_channel || List.first(channels)
      end

    current_index = App.Util.current_channel_index(channels, base_channel)
    next_index = rem(current_index + 1, length(channels))
    next_channel = Enum.at(channels, next_index)

    Logger.warning(
      "preview_next_channel " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "preview=#{inspect(state.preview_session && state.preview_session.target_channel && state.preview_session.target_channel.name)} " <>
        "base=#{inspect(base_channel && base_channel.name)} " <>
        "current_index=#{inspect(current_index)} " <>
        "next_index=#{inspect(next_index)} " <>
        "next=#{inspect(next_channel && next_channel.name)}"
    )

    request_preview_channel(state, base_channel, next_channel)
  end

  defp preview_prev_channel(%State{channels: []} = state), do: state

  defp preview_prev_channel(%State{channels: channels} = state) do
    base_channel =
      case state.preview_session do
        %PreviewSession{target_channel: channel} when not is_nil(channel) ->
          channel

        _ ->
          state.current_channel || List.first(channels)
      end

    current_index = App.Util.current_channel_index(channels, base_channel)
    prev_index = rem(current_index - 1 + length(channels), length(channels))
    prev_channel = Enum.at(channels, prev_index)

    Logger.warning(
      "preview_prev_channel " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "preview=#{inspect(state.preview_session && state.preview_session.target_channel && state.preview_session.target_channel.name)} " <>
        "base=#{inspect(base_channel && base_channel.name)} " <>
        "current_index=#{inspect(current_index)} " <>
        "prev_index=#{inspect(prev_index)} " <>
        "prev=#{inspect(prev_channel && prev_channel.name)}"
    )

    request_preview_channel(state, base_channel, prev_channel)
  end

  defp request_preview_channel(state, base_channel, channel) do
    cancel_preview_session_timer(state.preview_session)

    seq = state.next_seq + 1
    timer_ref = Process.send_after(self(), {:commit_preview, seq}, @preview_commit_ms)

    wrapped_to_current? = App.Util.same_channel?(channel, state.current_channel)

    Logger.warning(
      "request_preview_channel " <>
        "seq=#{inspect(seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "preview_before=#{inspect(state.preview_session && state.preview_session.target_channel && state.preview_session.target_channel.name)} " <>
        "requested=#{inspect(channel && channel.name)} " <>
        "wrapped_to_current?=#{inspect(wrapped_to_current?)}"
    )

    Osd.show(channel.name)

    %State{
      state
      | preview_session: %PreviewSession{
          seq: seq,
          base_channel: base_channel,
          target_channel: channel,
          timer_ref: timer_ref,
          wrapped_to_current?: wrapped_to_current?,
          switching?: false
        },
        next_seq: seq
    }
  end

  defp clear_preview_session(state) do
    cancel_preview_session_timer(state.preview_session)
    %{state | preview_session: nil}
  end

  defp cancel_preview_session_timer(%PreviewSession{timer_ref: timer_ref})
       when not is_nil(timer_ref) do
    App.Util.cancel_preview_timer(timer_ref)
  end

  defp cancel_preview_session_timer(_), do: :ok
end

defmodule App.PlaybackOwner do
  use GenServer
  require Logger

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, opts)
  end

  defmodule State do
    defstruct input_pid: nil,
              current_channel: nil,
              current_channel_id: nil,
              current_file_index: nil,
              current_player: nil,
              channel_state: %{},
              channels: [],
              image_display_timer: nil,
              next_prepared: nil,
              next_prepare_task: nil,
              next_prepare_ref: nil,
              next_requested_file_index: nil,
              display_elapsed?: false,
              switching?: false,
              switching_seq: nil
  end

  @impl true
  def init(opts) do
    Process.flag(:trap_exit, true)

    current_channel = Keyword.fetch!(opts, :current_channel)

    state = %State{
      input_pid: Keyword.fetch!(opts, :input_pid),
      current_channel: current_channel,
      current_channel_id: App.Util.channel_id(current_channel),
      channel_state: Keyword.fetch!(opts, :channel_state),
      channels: Keyword.fetch!(opts, :channels)
    }

    state =
      state
      |> App.Persistence.ensure_all_channel_state_entries()
      |> start_initial_playback()

    {:ok, state}
  end

  @impl true
  def handle_cast({:begin_switch, seq, channel}, state) do
    Logger.warning("begin_switch seq=#{inspect(seq)} channel=#{inspect(channel && channel.name)}")

    new_state =
      try do
        do_begin_channel_switch(
          %{state | switching?: true, switching_seq: seq},
          seq,
          channel
        )
      rescue
        error ->
          send(state.input_pid, {:playback_failed, channel, error})
          send(state.input_pid, {:switch_result, seq, {:error, error}, channel})
          %{state | switching?: false, switching_seq: nil}
      catch
        kind, reason ->
          send(state.input_pid, {:playback_failed, channel, {kind, reason}})
          send(state.input_pid, {:switch_result, seq, {:error, {kind, reason}}, channel})
          %{state | switching?: false, switching_seq: nil}
      end

    {:noreply, new_state}
  end

  @impl true
  def handle_cast(:quit, state) do
    {:stop, :normal, state}
  end

  @impl true
  def handle_info({:mpv_position, pid, seconds}, %{current_player: pid} = state) do
    current_path = App.Util.current_original_path(state)

    saved_offset_seconds =
      if PreparedMedia.image_file?(current_path) do
        0
      else
        App.Persistence.persisted_resume_offset(seconds)
      end

    state = App.Persistence.put_current_playhead(state, saved_offset_seconds)
    {:noreply, state}
  end

  def handle_info({:mpv_position, _pid, _seconds}, state), do: {:noreply, state}

  def handle_info(
        {:mpv_end_of_file, pid, reason},
        %{current_player: pid, switching?: true} = state
      ) do
    Logger.info(
      "mpv_end_of_file ignoring current-player eof during switch pid=#{inspect(pid)} reason=#{inspect(reason)}"
    )

    {:noreply, %{state | current_player: nil}}
  end

  def handle_info({:mpv_end_of_file, pid, reason}, %{current_player: pid} = state) do
    current_path = App.Util.current_original_path(state)

    Logger.info(
      "mpv_end_of_file: pid=#{inspect(pid)} reason=#{inspect(reason)} current_path=#{inspect(current_path)} image?=#{PreparedMedia.image_file?(current_path)} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared?=#{not is_nil(state.next_prepared)}"
    )

    if PreparedMedia.image_file?(current_path) do
      {:noreply, %{state | current_player: nil}}
    else
      {:noreply, App.Advance.advance_current_channel_item(%{state | current_player: nil})}
    end
  end

  def handle_info({:mpv_end_of_file, _pid, _reason}, state), do: {:noreply, state}

  def handle_info({:mpv_event, pid, :process_exit, payload}, %{current_player: pid} = state) do
    stopping? = Map.get(payload, :stopping?) || Map.get(payload, "stopping?")

    if stopping? do
      {:noreply, %{state | current_player: nil}}
    else
      state =
        case state.next_prepared do
          %{result: {:ok, _prepared}} ->
            App.Advance.advance_to_prepared_next(%{state | current_player: nil})

          _ ->
            App.Advance.advance_current_channel_item(%{state | current_player: nil})
        end

      {:noreply, state}
    end
  end

  def handle_info({:mpv_event, pid, :ipc_error, payload}, %{current_player: pid} = state) do
    Logger.error("player ipc error: #{inspect(payload)}")
    {:noreply, state}
  end

  def handle_info({:mpv_event, _pid, _kind, _payload}, state), do: {:noreply, state}

  def handle_info(:image_display_expired, state) do
    state =
      state
      |> Map.put(:image_display_timer, nil)
      |> Map.put(:display_elapsed?, true)

    {:noreply, App.Advance.maybe_advance(state)}
  end

  def handle_info(
        {ref, {:prepared_next_item, file_index, path, result}},
        %{next_prepare_ref: ref} = state
      ) do
    Process.demonitor(ref, [:flush])

    state =
      state
      |> clear_prepare_task_tracking()
      |> Map.put(:next_requested_file_index, file_index)
      |> Map.put(:next_prepared, %{file_index: file_index, path: path, result: result})

    {:noreply, App.Advance.maybe_advance(state)}
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, %{next_prepare_ref: ref} = state) do
    state =
      state
      |> clear_prepare_task_tracking()
      |> Map.put(:next_prepared, %{
        file_index: state.next_requested_file_index,
        path: expected_prepared_path(state, state.next_requested_file_index),
        result: {:error, {:prepare_task_down, reason}}
      })

    state =
      if PreparedMedia.image_file?(App.Util.current_original_path(state)) do
        App.Advance.maybe_advance(state)
      else
        state
      end

    {:noreply, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}

  defp do_begin_channel_switch(state, seq, requested_channel) do
    input_pid = state.input_pid

    state =
      state
      |> App.Advance.clear_transition_state()
      |> App.Persistence.persist_current_channel_position()

    state = App.Playback.stop_current_item(state)

    {file_index, offset_seconds, state} =
      App.Persistence.load_or_initialize_channel_position(state, requested_channel)

    case App.Playback.play_channel_item(state, requested_channel, file_index, offset_seconds) do
      {:ok, new_state, _actual_file_index} ->
        new_state =
          new_state
          |> Map.put(:switching?, false)
          |> Map.put(:switching_seq, nil)

        send(input_pid, {:playback_started, requested_channel})
        send(input_pid, {:switch_result, seq, :ok, requested_channel})

        new_state

      {:error, new_state, reason} ->
        new_state =
          new_state
          |> Map.put(:switching?, false)
          |> Map.put(:switching_seq, nil)

        send(input_pid, {:playback_failed, requested_channel, reason})
        send(input_pid, {:switch_result, seq, {:error, reason}, requested_channel})

        new_state
    end
  end

  defp start_initial_playback(%{current_channel: nil} = state), do: state

  defp start_initial_playback(state) do
    channel = state.current_channel

    {file_index, offset_seconds, state} =
      App.Persistence.load_or_initialize_channel_position(state, channel)

    case App.Playback.play_channel_item(state, channel, file_index, offset_seconds) do
      {:ok, new_state, _actual_file_index} ->
        send(state.input_pid, {:playback_started, channel})
        new_state

      {:error, new_state, reason} ->
        send(state.input_pid, {:playback_failed, channel, reason})
        new_state
    end
  end

  defp clear_prepare_task_tracking(state) do
    state
    |> Map.put(:next_prepare_task, nil)
    |> Map.put(:next_prepare_ref, nil)
  end

  defp expected_prepared_path(state, file_index) do
    case state.current_channel do
      %{media: media} when is_list(media) and is_integer(file_index) -> Enum.at(media, file_index)
      _ -> nil
    end
  end
end

defmodule App do
  require Logger

  def main(argv) do
    {opts, mediadirs, invalid} = OptionParser.parse(argv, strict: [scan: :boolean])

    cond do
      invalid != [] ->
        usage("invalid options: #{inspect(invalid)}")

      mediadirs == [] ->
        usage("missing media dirs")

      true ->
        mediadirs =
          mediadirs
          |> Enum.map(&Path.expand/1)
          |> Enum.uniq()

        scan? = Keyword.get(opts, :scan, false)

        saved_state = StateStore.load!()
        saved_channel_state = Map.fetch!(saved_state, "channel_state")
        saved_current_channel_id = Map.fetch!(saved_state, "current_channel_id")

        catalog = load_or_build_catalog!(mediadirs, scan?)
        channels = active_channels_for_roots(catalog, mediadirs)

        if channels == [] do
          usage("no playable media found in supplied media dirs")
        end

        current_channel =
          Enum.find(channels, fn channel ->
            App.Util.channel_id(channel) == saved_current_channel_id
          end) || List.first(channels)

        {:ok, _task_supervisor} =
          Task.Supervisor.start_link(name: App.TaskSupervisor)

        case Osd.start_link() do
          {:ok, _osd_pid} ->
            :ok

          {:error, {:already_started, _pid}} ->
            :ok
        end

        {:ok, input_pid} =
          App.Input.start_link(
            name: App.Input,
            channels: channels,
            current_channel: current_channel,
            playback_owner: nil
          )

        {:ok, playback_owner} =
          App.PlaybackOwner.start_link(
            name: App.PlaybackOwner,
            input_pid: input_pid,
            current_channel: current_channel,
            channel_state: saved_channel_state,
            channels: channels
          )

        :sys.replace_state(App.Input, fn state ->
          %{state | playback_owner: playback_owner}
        end)

        {:ok, runtime_sup} =
          RuntimeSupervisor.start_link(receiver: input_pid)

        ref1 = Process.monitor(input_pid)
        ref2 = Process.monitor(playback_owner)
        ref3 = Process.monitor(runtime_sup)

        receive do
          {:DOWN, ^ref1, :process, ^input_pid, reason} ->
            Logger.error("App.Input exited: #{inspect(reason)}")

          {:DOWN, ^ref2, :process, ^playback_owner, reason} ->
            Logger.error("App.PlaybackOwner exited: #{inspect(reason)}")

          {:DOWN, ^ref3, :process, ^runtime_sup, reason} ->
            Logger.error("RuntimeSupervisor exited: #{inspect(reason)}")
        end
    end
  end

  defp load_or_build_catalog!(mediadirs, true) do
    catalog =
      mediadirs
      |> Enum.reduce(%{}, fn rootdir, acc ->
        case DirectoryPlaylistLoader.scan_root(rootdir) do
          {:ok, channels} -> Map.put(acc, rootdir, %{"channels" => channels})
          {:error, _reason} -> acc
        end
      end)
      |> then(fn roots -> %{"version" => 1, "roots" => roots} end)

    :ok = CatalogStore.save!(catalog)
    catalog
  end

  defp load_or_build_catalog!(mediadirs, false) do
    catalog = CatalogStore.load!()
    roots = Map.get(catalog, "roots", %{})

    {updated_roots, changed?} =
      Enum.reduce(mediadirs, {roots, false}, fn rootdir, {acc, changed?} ->
        if Map.has_key?(acc, rootdir) do
          {acc, changed?}
        else
          case DirectoryPlaylistLoader.scan_root(rootdir) do
            {:ok, channels} -> {Map.put(acc, rootdir, %{"channels" => channels}), true}
            {:error, _reason} -> {acc, changed?}
          end
        end
      end)

    updated_catalog = %{"version" => 1, "roots" => updated_roots}
    if changed?, do: CatalogStore.save!(updated_catalog)
    updated_catalog
  end

  defp active_channels_for_roots(catalog, mediadirs) do
    roots = Map.get(catalog, "roots", %{})

    mediadirs
    |> Enum.flat_map(fn rootdir ->
      root_entry = Map.get(roots, rootdir, %{"channels" => %{}})
      channels = Map.get(root_entry, "channels", %{})

      channels
      |> Enum.map(fn {_channel_id, channeldata} ->
        %{
          root: rootdir,
          name: Map.fetch!(channeldata, "name"),
          path: Map.fetch!(channeldata, "path"),
          media: Map.fetch!(channeldata, "media"),
          label: "#{Map.fetch!(channeldata, "name")} (#{rootdir})"
        }
      end)
    end)
    |> Enum.sort_by(&{&1.root, &1.name, &1.path})
  end

  defp usage(message) do
    IO.puts(message)
    IO.puts("usage: #{Script.name()} [--scan] MEDIA_DIR [MEDIA_DIR ...]")
    System.halt(1)
  end
end

App.main(System.argv())
