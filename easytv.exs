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
    ".mp3", ".flac", ".m4a", ".m4b", ".aac", ".ac3", ".ogg", ".oga", ".opus",
    ".wav", ".aiff", ".alac", ".ape", ".amr", ".wma", ".wv", ".tta",
    ".mp1", ".mp2", ".mpa", ".mid", ".midi", ".xm", ".it", ".s3m", ".mod",
    ".ra", ".rmi", ".spx", ".voc"
  ])

  @video_extensions MapSet.new([
    ".rm", ".mp4", ".m4v", ".mkv", ".mov", ".avi", ".webm", ".flv", ".wmv",
    ".mpg", ".mpeg", ".mpv", ".m1v", ".m2v", ".ts", ".m2ts", ".mts", ".vob",
    ".ogv", ".ogm", ".3gp", ".3g2", ".asf", ".divx", ".dv", ".f4v", ".mxf",
    ".nuv", ".rmvb", ".tod", ".trp", ".vro"
  ])

  @image_extensions MapSet.new([
    ".jpg", ".jpeg", ".jpe", ".jfif", ".jif", ".png", ".gif", ".bmp", ".tif",
    ".tiff", ".webp", ".ico", ".pbm", ".pgm", ".ppm", ".pnm", ".tga"
  ])

  @media_extensions MapSet.union(@audio_extensions, @video_extensions) |> MapSet.union(@image_extensions)

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
        IO.puts(
          "JsonFileStore.save!: rename failed from #{tmp} to #{path}: #{inspect(reason)}"
        )

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

  def validate_state!(%{
        "version" => @version,
        "current_channel_id" => current_channel_id,
        "channel_state" => channel_state
      } = data)
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

  def prepare(path) when is_binary(path) do
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
          [tmp_source, "--size", "#{max_dim}x#{max_dim}", "--linear", "-o", "#{tmp_output}[Q=90]"],
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

  defp sweep_tmp_dir(dir, keep_last) when is_binary(dir) and is_integer(keep_last) and keep_last >= 0 do
    case File.ls(dir) do
      {:ok, entries} ->
        entries
        |> Enum.map(&Path.join(dir, &1))
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort_by(&mtime/1, :desc)
        |> Enum.drop(keep_last)
        |> Enum.each(fn path ->
          case File.rm(path) do
            :ok -> :ok
            {:error, :enoent} -> :ok
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
      :ok -> :ok
      {:error, :enoent} -> :ok
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

        Logger.error(
          "mpv request timed out request_id=#{request_id} tag=#{inspect(tag)} age_ms=#{age_ms} command=#{inspect(command)}"
        )

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
    Script.tmp_file("mpv-player-#{System.os_time(:millisecond)}-#{:erlang.unique_integer([:positive])}.sock")
  end

  defp find_executable!(name) do
    System.find_executable(name) || raise "could not find executable: #{name}"
  end

  defp connect_until(_socket_path, remaining) when remaining <= 0, do: {:error, :timeout}

  defp connect_until(socket_path, remaining) do
    case :gen_tcp.connect({:local, String.to_charlist(socket_path)}, 0, [:binary, active: :once], @connect_retry_ms) do
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
        timer_ref = Process.send_after(self(), {:request_timeout, request_id}, @request_timeout_ms)

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
    {_result, state} = send_command(state, ["observe_property", id, name], {:observe_property, name})
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

      {:ok, %{"request_id" => request_id, "error" => "success"} = msg} when is_integer(request_id) ->
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
        Supervisor.child_spec({InputDevice, [path: path, receiver: receiver]}, id: {:input_device, path})
      end)

    children = input_children

    Supervisor.init(children, strategy: :one_for_one)
  end
end

defmodule ScriptRunner do
  require Logger

  def run(script_name, args) when is_binary(script_name) and is_list(args) do
    script = Path.join(Path.dirname(Path.expand(__ENV__.file)), script_name)

    case File.regular?(script) do
      true ->
        case System.cmd(script, args, stderr_to_stdout: true) do
          {output, 0} ->
            log_output(script_name, output, :debug)
            :ok

          {output, status} ->
            log_output(script_name, output, :error)
            {:error, {:script_failed, script_name, status, String.trim(output)}}
        end

      false ->
        {:error, {:script_missing, script}}
    end
  end

  defp log_output(script_name, output, level) do
    trimmed = String.trim(output)

    if trimmed != "" do
      Logger.log(level, "#{script_name}: #{trimmed}")
    end
  end
end

defmodule Osd do
  def show(text) when is_binary(text) do
    ScriptRunner.run("easytv-osd.sh", [text])
  end

  def clear do
    ScriptRunner.run("easytv-osd.sh", [])
  end
end

defmodule Volume do
  def toggle_mute, do: ScriptRunner.run("easytv-volume.sh", [])
  def increase, do: ScriptRunner.run("easytv-volume.sh", ["+"])
  def decrease, do: ScriptRunner.run("easytv-volume.sh", ["-"])
end

defmodule Power do
  def tv_off, do: ScriptRunner.run("easytv-power.sh", ["0"])
  def tv_on, do: ScriptRunner.run("easytv-power.sh", ["1"])
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
  def current_original_path(state), do: Enum.at(state.current_channel.media, state.current_file_index)

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
    Enum.find_index(channels, &same_channel?(&1, channel)) || 0
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

    %{state | channel_state: put_channel_position(state.channel_state, state.current_channel_id, current_path, normalized_offset)}
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
        "preview_active?=#{inspect(state.preview_active?)} " <>
        "next_prepare_ref=#{inspect(state.next_prepare_ref)} " <>
        "next_prepared?=#{inspect(not is_nil(state.next_prepared))}"
    )

    state
    |> App.Advance.clear_transition_state()
    |> App.Preview.cancel_active_preview()
    |> App.Persistence.persist_current_channel_position()
    |> stop_current_item()
  end

  def resume_playback(state) do
    Logger.info(
      "resume_playback: " <>
        "current_channel=#{inspect(state.current_channel && state.current_channel.label)} " <>
        "saved_current_channel_id=#{inspect(state.current_channel_id)}"
    )

    state =
      state
      |> App.Advance.clear_transition_state()
      |> App.Preview.cancel_active_preview()

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

            # Osd.show(channel.name)
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

  def stop_current_item(%{current_player: nil} = state), do: state

  def stop_current_item(%{current_player: player} = state) do
    Logger.info(
      "stop_current_item: player=#{inspect(player)} current_path=#{inspect(App.Util.current_original_path(state))}"
    )

    try do
      MpvPlayer.stop(player)
    catch
      :exit, {:noproc, _} -> :ok
      :exit, _reason -> :ok
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

      not File.regular?(path) ->
        Logger.warning("skipping missing file for #{channel.label}: #{inspect(path)}")

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
        actual_offset =
          playback_offset_for_candidate(path, candidate_index, requested_index, offset_seconds)

        Logger.debug(
          "try_play_channel_item: preparing channel=#{channel.label} candidate_index=#{candidate_index} path=#{inspect(path)} actual_offset=#{actual_offset} image?=#{PreparedMedia.image_file?(path)}"
        )

        with {:ok, prepared} <- PreparedMedia.prepare(path),
             {:ok, new_state, _} <-
               start_current_item(state, channel, candidate_index, prepared, actual_offset) do
          actual_path = Enum.at(media, candidate_index)

          Logger.info(
            "playing #{channel.label}: #{inspect(actual_path)} candidate_index=#{candidate_index} requested_index=#{inspect(requested_index)}"
          )

          {:ok, new_state, candidate_index}
        else
          {:error, reason} ->
            Logger.warning(
              "try_play_channel_item: play failed channel=#{channel.label} path=#{inspect(path)} candidate_index=#{candidate_index} reason=#{inspect(reason)}; trying next candidate"
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
    try_play_channel_item(
      state,
      channel,
      media,
      requested_index,
      offset_seconds,
      App.Util.next_file_index(candidate_index, media),
      attempts + 1,
      max_attempts
    )
  end

  defp playback_offset_for_candidate(path, candidate_index, requested_index, offset_seconds) do
    cond do
      PreparedMedia.image_file?(path) ->
        0

      candidate_index == requested_index ->
        App.Util.normalize_offset(offset_seconds)

      true ->
        0
    end
  end
end

defmodule App.Preview do
  require Logger

  @preview_commit_ms 500

  def cancel_active_preview(%{preview_active?: false} = state), do: state

  def cancel_active_preview(state) do
    App.Util.cancel_preview_timer(state.preview_timer)
    clear_preview_state(state)
  end

  def preview_next_channel(%{channels: []} = state), do: state

  def preview_next_channel(%{channels: channels} = state) do
    base_channel = state.preview_channel || state.current_channel
    current_index = App.Util.current_channel_index(channels, base_channel)
    next_index = rem(current_index + 1, length(channels))
    request_preview_channel(state, Enum.at(channels, next_index))
  end

  def preview_prev_channel(%{channels: []} = state), do: state

  def preview_prev_channel(%{channels: channels} = state) do
    base_channel = state.preview_channel || state.current_channel
    current_index = App.Util.current_channel_index(channels, base_channel)
    prev_index = rem(current_index - 1 + length(channels), length(channels))
    request_preview_channel(state, Enum.at(channels, prev_index))
  end

  def commit_preview(%{requested_channel: nil} = state) do
    Logger.debug("commit_preview: no requested channel")
    {:noop, clear_preview_state(state)}
  end

  def commit_preview(%{requested_channel: requested_channel} = state) do
    if App.Util.same_channel?(requested_channel, state.current_channel) do
      Logger.debug("commit_preview: requested channel already current")
      {:noop, clear_preview_state(state)}
    else
      Logger.info("commit_preview: committing requested_channel=#{requested_channel.label}")
      {:switch_channel, requested_channel, clear_preview_state(state)}
    end
  end

  def clear_preview_state(state) do
    state
    |> Map.put(:preview_channel, nil)
    |> Map.put(:requested_channel, nil)
    |> Map.put(:preview_timer, nil)
    |> Map.put(:preview_active?, false)
  end

  defp request_preview_channel(state, channel) do
    App.Util.cancel_preview_timer(state.preview_timer)
    timer_ref = Process.send_after(self(), :commit_preview, @preview_commit_ms)

    if not App.Util.same_channel?(channel, state.preview_channel) do
      Osd.show(channel.name)
    end

    Logger.debug(
      "request_preview_channel: requested_channel=#{channel.label} timer_ref=#{inspect(timer_ref)}"
    )

    state
    |> Map.put(:preview_channel, channel)
    |> Map.put(:requested_channel, channel)
    |> Map.put(:preview_timer, timer_ref)
    |> Map.put(:preview_active?, true)
  end
end

defmodule App.Advance do
  require Logger

  @slideshow_image_display_ms 10_000

  def maybe_advance(%{current_channel: nil} = state) do
    Logger.debug("maybe_advance: no current channel")
    state
  end

  def maybe_advance(state) do
    current_path = App.Util.current_original_path(state)
    image? = PreparedMedia.image_file?(current_path)

    Logger.debug(
      "maybe_advance: current_path=#{inspect(current_path)} image?=#{image?} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared=#{inspect(state.next_prepared && Map.take(state.next_prepared, [:file_index, :path, :result]))}"
    )

    cond do
      image? and state.display_elapsed? and not is_nil(state.next_prepared) ->
        Logger.info("maybe_advance: conditions met, advancing to prepared next item")
        advance_to_prepared_next(state)

      true ->
        Logger.debug("maybe_advance: conditions not met, waiting")
        state
    end
  end

  def advance_to_prepared_next(
        %{next_prepared: %{file_index: file_index, result: {:ok, prepared}}} = state
      ) do
    channel = state.current_channel
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "advance_to_prepared_next: channel=#{channel.label} file_index=#{file_index} actual_path=#{inspect(actual_path)} prepared_play_path=#{inspect(prepared.play_path)}"
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
          "failed starting prepared next item #{inspect(actual_path)}: #{inspect(reason)}"
        )

        advance_current_channel_item(state)
    end
  end

  def advance_to_prepared_next(%{next_prepared: %{path: path, result: {:error, reason}}} = state) do
    Logger.error("async prepare failed for next item #{inspect(path)}: #{inspect(reason)}")

    state
    |> clear_transition_state()
    |> advance_current_channel_item()
  end

  def advance_to_prepared_next(state) do
    Logger.debug(
      "advance_to_prepared_next: no usable prepared item next_prepared=#{inspect(state.next_prepared)}"
    )

    state
  end

  def advance_current_channel_item(%{current_channel: nil} = state) do
    Logger.debug("advance_current_channel_item: skip no current channel")
    state
  end

  def advance_current_channel_item(state) do
    channel = state.current_channel
    requested_file_index = App.Util.next_file_index(state.current_file_index, channel.media)

    Logger.info(
      "advance_current_channel_item: channel=#{channel.label} current_index=#{inspect(state.current_file_index)} requested_next_index=#{requested_file_index} current_player=#{inspect(state.current_player)}"
    )

    state = App.Playback.stop_current_item(state)

    case App.Playback.play_channel_item(state, channel, requested_file_index, 0) do
      {:ok, state, actual_file_index} ->
        actual_path = Enum.at(channel.media, actual_file_index)

        Logger.info(
          "advance_current_channel_item: started channel=#{channel.label} actual_index=#{actual_file_index} actual_path=#{inspect(actual_path)}"
        )

        state
        |> App.Persistence.put_current_playhead(0)
        |> App.Persistence.persist_state_file!()

      {:error, state, reason} ->
        Logger.error(
          "advance_current_channel_item: failed channel=#{channel.label} requested_next_index=#{requested_file_index} reason=#{inspect(reason)}"
        )

        %{state | current_player: nil}
    end
  end

  def maybe_start_image_display_timer(state, path) do
    App.Util.cancel_image_display_timer(state.image_display_timer)

    if PreparedMedia.image_file?(path) do
      timer_ref = Process.send_after(self(), :image_display_expired, @slideshow_image_display_ms)

      Logger.debug(
        "maybe_start_image_display_timer: armed path=#{inspect(path)} timer_ref=#{inspect(timer_ref)} display_ms=#{@slideshow_image_display_ms}"
      )

      %{state | image_display_timer: timer_ref, display_elapsed?: false}
    else
      Logger.debug(
        "maybe_start_image_display_timer: skipped non-image path=#{inspect(path)}"
      )

      %{state | image_display_timer: nil, display_elapsed?: false}
    end
  end

  def maybe_prepare_next_item(%{current_channel: nil} = state) do
    Logger.debug("maybe_prepare_next_item: skip no current channel")
    state
  end

  def maybe_prepare_next_item(%{next_prepare_task: task} = state) when not is_nil(task) do
    Logger.debug("maybe_prepare_next_item: skip task already running")
    state
  end

  def maybe_prepare_next_item(%{next_prepared: prepared} = state) when not is_nil(prepared) do
    Logger.debug("maybe_prepare_next_item: skip next item already prepared")
    state
  end

  def maybe_prepare_next_item(state) do
    media = state.current_channel.media

    if media == [] or not is_integer(state.current_file_index) do
      Logger.debug(
        "maybe_prepare_next_item: skip media_empty?=#{inspect(media == [])} current_file_index=#{inspect(state.current_file_index)}"
      )

      state
    else
      next_index = App.Util.next_file_index(state.current_file_index, media)
      next_path = Enum.at(media, next_index)

      Logger.debug(
        "maybe_prepare_next_item: starting async prepare next_index=#{next_index} next_path=#{inspect(next_path)} current_index=#{inspect(state.current_file_index)} current_path=#{inspect(App.Util.current_original_path(state))} screen_off?=#{inspect(state.screen_off?)}"
      )

      start_prepare_next_task(state, next_index, next_path)
    end
  end

  def clear_transition_state(state) do
    Logger.debug(
      "clear_transition_state: clearing image timer and prepared-next state current_path=#{inspect(App.Util.current_original_path(state))}"
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
    # Do not Task.await/shutdown here; this can be called from any process.
    # Just drop the references from the state. The task will exit on its own.
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
        result = PreparedMedia.prepare(next_path)
        elapsed_ms = System.monotonic_time(:millisecond) - started_ms

        Logger.info(
          "maybe_prepare_next_item: finished async prepare next_index=#{next_index} next_path=#{inspect(next_path)} elapsed_ms=#{elapsed_ms} result=#{inspect(result)}"
        )

        send(owner, {ref, {:prepared_next_item, next_index, next_path, result}})
      end)

    state
    |> Map.put(:next_prepare_task, task)
    |> Map.put(:next_prepare_ref, ref)
    |> Map.put(:next_requested_file_index, next_index)
  end
end

defmodule App.ChannelSwitcher do
  use GenServer

  defmodule State do
    defstruct next_request_id: 1,
              latest_request_id: nil,
              latest_channel: nil,
              in_flight_request_id: nil,
              in_flight_channel: nil
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: opts[:name])
  end

  def request_switch(server, channel) do
    GenServer.call(server, {:request_switch, channel})
  end

  def begin_next_switch(server) do
    GenServer.call(server, :begin_next_switch)
  end

  def finish_switch(server, request_id) do
    GenServer.call(server, {:finish_switch, request_id})
  end

  def peek_latest(server) do
    GenServer.call(server, :peek_latest)
  end

  @impl true
  def init(_opts) do
    {:ok, %State{}}
  end

  @impl true
  def handle_call({:request_switch, channel}, _from, state) do
    request_id = state.next_request_id

    state = %State{
      state
      | next_request_id: request_id + 1,
        latest_request_id: request_id,
        latest_channel: channel
    }

    {:reply, {:ok, request_id}, state}
  end

  def handle_call(:begin_next_switch, _from, %{latest_request_id: nil} = state) do
    {:reply, :none, state}
  end

  def handle_call(:begin_next_switch, _from, %{in_flight_request_id: request_id} = state)
      when not is_nil(request_id) do
    {:reply, :busy, state}
  end

  def handle_call(:begin_next_switch, _from, state) do
    state = %State{
      state
      | in_flight_request_id: state.latest_request_id,
        in_flight_channel: state.latest_channel
    }

    {:reply, {:ok, state.in_flight_request_id, state.in_flight_channel}, state}
  end

  def handle_call({:finish_switch, request_id}, _from, state) do
    stale? =
      not is_nil(state.latest_request_id) and request_id < state.latest_request_id

    state =
      if state.in_flight_request_id == request_id do
        %State{state | in_flight_request_id: nil, in_flight_channel: nil}
      else
        state
      end

    reply =
      if stale? do
        :stale_reschedule
      else
        :apply_current
      end

    {:reply, reply, state}
  end

  def handle_call(:peek_latest, _from, state) do
    {:reply, {state.latest_request_id, state.latest_channel}, state}
  end
end

defmodule App do
  use GenServer
  require Logger

  def main(argv) do
    {opts, mediadirs, invalid} = OptionParser.parse(argv, strict: [scan: :boolean])

    cond do
      invalid != [] ->
        usage("invalid options: #{inspect(invalid)}")

      mediadirs == [] ->
        usage("missing media dirs")

      true ->
        mediadirs = mediadirs |> Enum.map(&Path.expand/1) |> Enum.uniq()
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

        init_state =
          build_init_state(mediadirs, catalog, channels, current_channel, saved_channel_state)

        {:ok, pid} = GenServer.start_link(__MODULE__, init_state)
        ref = Process.monitor(pid)

        receive do
          {:DOWN, ^ref, :process, ^pid, _reason} -> :ok
        end
    end
  end

  @impl true
  def init(state) do
    Process.flag(:trap_exit, true)
    state = if Keyword.keyword?(state), do: Enum.into(state, %{}), else: state

    {:ok, runtime_sup} = RuntimeSupervisor.start_link(receiver: self())
    {:ok, channel_switcher} = App.ChannelSwitcher.start_link()

    state =
      state
      |> Map.put(:runtime_sup, runtime_sup)
      |> Map.put(:channel_switcher, channel_switcher)
      |> Map.put(:screen_off?, false)
      |> App.Persistence.ensure_all_channel_state_entries()

    case Power.tv_on() do
      :ok ->
        Logger.info("init: tv on")

      {:error, reason} ->
        Logger.error("init: tv on failed #{inspect(reason)}")
    end

    {:ok, start_initial_playback(state)}
  end

  @impl true
  def handle_info({:input_event, _dev, events}, state) when is_list(events) do
    state =
      if state.screen_off? and App.Util.wake_keypress?(events) do
        wake_screen(state)
      else
        Enum.reduce(events, state, fn
          {:ev_key, :key_pageup, 1}, state ->
            Logger.info("PgUp -> prev channel")
            App.Preview.preview_prev_channel(state)

          {:ev_key, :key_pagedown, 1}, state ->
            Logger.info("PgDn -> next channel")
            App.Preview.preview_next_channel(state)

          {:ev_key, :key_space, 1}, state ->
            toggle_screen(state)

          {:ev_key, :key_q, 1}, state ->
            Logger.info("Q -> quit")
            Process.send_after(self(), :quit, 0)
            state

          {:ev_key, :key_m, 1}, state ->
            Logger.info("M -> toggle mute")
            _ = Volume.toggle_mute()
            state

          {:ev_key, :key_minus, 1}, state ->
            Logger.info("- -> volume down")
            _ = Volume.decrease()
            state

          {:ev_key, :key_kpminus, 1}, state ->
            Logger.info("Numpad - -> volume down")
            _ = Volume.decrease()
            state

          {:ev_key, :key_equal, 1}, state ->
            Logger.info("= -> volume up")
            _ = Volume.increase()
            state

          {:ev_key, :key_plus, 1}, state ->
            Logger.info("+ -> volume up")
            _ = Volume.increase()
            state

          {:ev_key, :key_kpplus, 1}, state ->
            Logger.info("Numpad + -> volume up")
            _ = Volume.increase()
            state

          _, state ->
            state
        end)
      end

    {:noreply, state}
  end

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

  def handle_info({:mpv_end_of_file, pid, reason}, %{current_player: pid} = state) do
    current_path = App.Util.current_original_path(state)

    Logger.info(
      "mpv_end_of_file: pid=#{inspect(pid)} reason=#{inspect(reason)} current_path=#{inspect(current_path)} image?=#{PreparedMedia.image_file?(current_path)} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared?=#{not is_nil(state.next_prepared)}"
    )

    if PreparedMedia.image_file?(current_path) do
      Logger.debug("mpv_end_of_file: image item reached end-of-file, clearing current_player only")
      {:noreply, %{state | current_player: nil}}
    else
      Logger.info("mpv_end_of_file: advancing non-image item")
      {:noreply, App.Advance.advance_current_channel_item(%{state | current_player: nil})}
    end
  end

  def handle_info({:mpv_end_of_file, _pid, reason}, state) do
    Logger.debug(
      "mpv_end_of_file: ignoring stale event reason=#{inspect(reason)} current_player=#{inspect(state.current_player)}"
    )

    {:noreply, state}
  end

  def handle_info({:mpv_event, pid, :process_exit, payload}, %{current_player: pid} = state) do
    stopping? = Map.get(payload, :stopping?) || Map.get(payload, "stopping?")

    Logger.info(
      "mpv process_exit: pid=#{inspect(pid)} stopping?=#{inspect(stopping?)} payload=#{inspect(payload)} current_channel=#{inspect(state.current_channel && state.current_channel.label)} next_prepared=#{inspect(state.next_prepared && Map.take(state.next_prepared, [:file_index, :path]))}"
    )

    if stopping? do
      Logger.debug("mpv process_exit: expected stop, clearing current_player")
      {:noreply, %{state | current_player: nil}}
    else
      Logger.error(
        "playback exited unexpectedly for #{state.current_channel.label}: #{inspect(payload)}"
      )

      state =
        case state.next_prepared do
          %{result: {:ok, _prepared}} ->
            Logger.info("mpv process_exit: attempting prepared-next recovery")
            App.Advance.advance_to_prepared_next(%{state | current_player: nil})

          _ ->
            Logger.info("mpv process_exit: falling back to normal advance")
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
    Logger.info(
      "image_display_expired: current_path=#{inspect(App.Util.current_original_path(state))} current_index=#{inspect(state.current_file_index)} next_prepare_ref=#{inspect(state.next_prepare_ref)} next_prepared?=#{not is_nil(state.next_prepared)}"
    )

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
    expected_path = expected_prepared_path(state, file_index)

    Logger.debug(
      "prepared-next result received ref=#{inspect(ref)} index=#{inspect(file_index)} path=#{inspect(path)} expected_path=#{inspect(expected_path)} path_match?=#{path == expected_path} result=#{inspect(result)}"
    )

    Process.demonitor(ref, [:flush])

    state =
      state
      |> clear_prepare_task_tracking()
      |> Map.put(:next_requested_file_index, file_index)
      |> Map.put(:next_prepared, %{file_index: file_index, path: path, result: result})

    {:noreply, App.Advance.maybe_advance(state)}
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, %{next_prepare_ref: ref} = state) do
    Logger.warning(
      "prepared-next task down ref=#{inspect(ref)} requested_index=#{inspect(state.next_requested_file_index)} reason=#{inspect(reason)}"
    )

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
        Logger.debug("prepared-next task down while image showing; trying maybe_advance")
        App.Advance.maybe_advance(state)
      else
        state
      end

    {:noreply, state}
  end

  def handle_info(:commit_preview, state) do
    case App.Preview.commit_preview(state) do
      {:noop, new_state} ->
        {:noreply, new_state}

      {:switch_channel, requested_channel, new_state} ->
        {:ok, _request_id} =
          App.ChannelSwitcher.request_switch(new_state.channel_switcher, requested_channel)

        case App.ChannelSwitcher.begin_next_switch(new_state.channel_switcher) do
          {:ok, request_id, channel} ->
            {:noreply, do_begin_channel_switch(new_state, request_id, channel)}

          :busy ->
            {:noreply, new_state}

          :none ->
            {:noreply, new_state}
        end
    end
  end

  def handle_info(:quit, state) do
    Logger.info("Q - quitting")
    Osd.clear()

    state =
      state
      |> App.Advance.clear_transition_state()
      |> App.Preview.cancel_active_preview()
      |> App.Persistence.persist_current_channel_position()
      |> App.Playback.stop_current_item()

    {:stop, :normal, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}

  defp do_begin_channel_switch(state, request_id, requested_channel) do
    Logger.info(
      "do_begin_channel_switch: request_id=#{request_id} requested_channel=#{requested_channel.label}"
    )

    state =
      state
      |> App.Advance.clear_transition_state()
      |> App.Persistence.persist_current_channel_position()

    state = App.Playback.stop_current_item(state)

    {file_index, offset_seconds, state} =
      App.Persistence.load_or_initialize_channel_position(state, requested_channel)

    case App.Playback.play_channel_item(state, requested_channel, file_index, offset_seconds) do
      {:ok, new_state, actual_file_index} ->
        actual_path = Enum.at(requested_channel.media, actual_file_index)

        Logger.info(
          "do_begin_channel_switch: started channel=#{requested_channel.label} actual_file=#{inspect(actual_path)} offset=#{offset_seconds}s request_id=#{request_id}"
        )

        maybe_apply_or_cancel_stale_switch(new_state, request_id, requested_channel)

      {:error, new_state, reason} ->
        Logger.error(
          "do_begin_channel_switch: failed channel=#{requested_channel.label} reason=#{inspect(reason)} request_id=#{request_id}"
        )

        handle_channel_switch_failure(new_state, request_id)
    end
  end

  defp maybe_apply_or_cancel_stale_switch(state, request_id, requested_channel) do
    {latest_request_id, latest_channel} =
      App.ChannelSwitcher.peek_latest(state.channel_switcher)

    stale? =
      latest_request_id != request_id or
        not App.Util.same_channel?(latest_channel, requested_channel)

    if stale? do
      Logger.info(
        "maybe_apply_or_cancel_stale_switch: stale started switch request_id=#{request_id} latest_request_id=#{inspect(latest_request_id)}; stopping just-started player"
      )

      state
      |> App.Playback.stop_current_item()
      |> handle_channel_switch_failure(request_id)
    else
      handle_channel_switch_completion(state, request_id)
    end
  end

  defp handle_channel_switch_completion(state, request_id) do
    case App.ChannelSwitcher.finish_switch(state.channel_switcher, request_id) do
      :apply_current ->
        App.Preview.clear_preview_state(state)

      :stale_reschedule ->
        schedule_immediate_commit()
        state
    end
  end

  defp handle_channel_switch_failure(state, request_id) do
    case App.ChannelSwitcher.finish_switch(state.channel_switcher, request_id) do
      :apply_current ->
        state

      :stale_reschedule ->
        schedule_immediate_commit()
        state
    end
  end

  defp schedule_immediate_commit do
    Process.send_after(self(), :commit_preview, 0)
    :ok
  end

  defp build_init_state(mediadirs, catalog, channels, current_channel, saved_channel_state) do
    %{
      mediadirs: mediadirs,
      catalog: catalog,
      channels: channels,
      current_channel: current_channel,
      current_channel_id: App.Util.channel_id(current_channel),
      current_file_index: nil,
      current_player: nil,
      preview_channel: nil,
      requested_channel: nil,
      preview_timer: nil,
      preview_active?: false,
      screen_off?: false,
      channel_state: saved_channel_state,
      runtime_sup: nil,
      channel_switcher: nil,
      image_display_timer: nil,
      next_prepared: nil,
      next_prepare_task: nil,
      next_prepare_ref: nil,
      next_requested_file_index: nil,
      display_elapsed?: false
    }
  end

  defp start_initial_playback(%{current_channel: nil} = state), do: state

  defp start_initial_playback(state) do
    channel = state.current_channel

    {file_index, offset_seconds, state} =
      App.Persistence.load_or_initialize_channel_position(state, channel)

    case App.Playback.play_channel_item(state, channel, file_index, offset_seconds) do
      {:ok, new_state, actual_file_index} ->
        actual_path = Enum.at(channel.media, actual_file_index)
        Logger.info("current channel: #{channel.label}")
        Logger.info("resume position: file=#{inspect(actual_path)} offset=#{offset_seconds}s")
        Osd.show(channel.name)
        new_state

      {:error, new_state, reason} ->
        Logger.error("startup play failed: #{inspect(reason)}")
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

  defp toggle_screen(%{screen_off?: true} = state), do: wake_screen(state)
  defp toggle_screen(state), do: turn_screen_off(state)

  defp turn_screen_off(state) do
    state = App.Persistence.persist_current_channel_position(state)

    case Power.tv_off() do
      :ok -> Logger.info("tv off")
      {:error, reason} -> Logger.error("tv off failed #{inspect(reason)}")
    end

    Map.put(state, :screen_off?, true)
  end

  defp wake_screen(state) do
    case Power.tv_on() do
      :ok ->
        Logger.info("wake_screen: tv on")
        Map.put(state, :screen_off?, false)

      {:error, reason} ->
        Logger.error("wake_screen: tv on failed #{inspect(reason)}")
        state
    end
  end

  defp usage(message) do
    IO.puts(message)
    IO.puts("usage: #{Script.name()} [--scan] MEDIA_DIR [MEDIA_DIR ...]")
    System.halt(1)
  end
end

App.main(System.argv())
