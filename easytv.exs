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
    File.mkdir_p!(Path.dirname(path))

    json = Jason.encode_to_iodata!(data, pretty: true)
    tmp = path <> ".tmp"

    File.write!(tmp, json)
    File.rename!(tmp, path)
    :ok
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

  @max_image_dimension 3840
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
      :stop_from,
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
  def stop(server), do: GenServer.call(server, :stop, 10_000)

  @impl true
  def init(opts) do
    parent = Keyword.fetch!(opts, :parent)
    file = Keyword.fetch!(opts, :file)
    start_pos = Keyword.get(opts, :start_pos, 0)
    socket_path = socket_path()

    File.rm(socket_path)

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
      "--cache=yes",
      "--cache-secs=30",
      "--stream-buffer-size=2MiB",
      "--demuxer-max-bytes=150MiB",
      "--demuxer-max-back-bytes=50MiB"
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

    state = %State{
      parent: parent,
      port: port,
      port_os_pid: port_os_pid,
      socket_path: socket_path,
      file: file,
      start_pos: start_pos,
      connected?: false,
      stopping?: false,
      stop_from: nil,
      last_position: nil,
      buffer: ""
    }

    case connect_until(socket_path, @startup_timeout_ms) do
      {:ok, socket} ->
        Logger.info("mpv started os_pid=#{inspect(port_os_pid)} socket=#{socket_path}")
        send_event(state, :started, %{os_pid: port_os_pid, socket_path: socket_path, file: file})

        state = %{state | socket: socket, connected?: true}
        state = send_observe_property(state, 2, "pause")

        Process.send_after(self(), :poll_status, @poll_interval)
        {:ok, state}

      {:error, reason} ->
        Logger.error("failed to connect to mpv ipc: #{inspect(reason)}")
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

  def handle_call(:stop, from, state) do
    {result, state} = send_command(state, ["quit"], :stop)

    case result do
      {:ok, _request_id} ->
        {:noreply, %{state | stopping?: true, stop_from: from}}

      {:error, _reason} ->
        {:reply, result, %{state | stopping?: true}}
    end
  end

  @impl true
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

        state = %{state | pending: pending}

        state =
          if tag == :stop and state.stop_from do
            GenServer.reply(state.stop_from, {:error, :timeout})
            %{state | stop_from: nil}
          else
            state
          end

        {:noreply, state}
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
    Logger.info("mpv exited with status #{status}")
    send_event(state, :process_exit, %{status: status, stopping?: state.stopping?})

    state =
      if state.stop_from do
        GenServer.reply(state.stop_from, :ok)
        %{state | stop_from: nil}
      else
        state
      end

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

    state =
      if state.stop_from do
        reply = if expected?, do: :ok, else: {:error, :ipc_closed}
        GenServer.reply(state.stop_from, reply)
        %{state | stop_from: nil}
      else
        state
      end

    {:stop, :normal, %{state | connected?: false, socket: nil, pending: %{}}}
  end

  def handle_info({:tcp_error, socket, reason}, %{socket: socket} = state) do
    Logger.error("mpv ipc socket error: #{inspect(reason)}")
    send_event(state, :ipc_error, %{stage: :socket_error, reason: inspect(reason)})

    state =
      if state.stop_from do
        GenServer.reply(state.stop_from, {:error, reason})
        %{state | stop_from: nil}
      else
        state
      end

    {:stop, :normal, %{state | connected?: false, socket: nil, pending: %{}}}
  end

  def handle_info(msg, state) do
    Logger.debug("unhandled message: #{inspect(msg)}")
    {:noreply, state}
  end

  @impl true
  def terminate(reason, state) do
    if state.socket, do: safe_socket_close(state.socket)

    if state.socket_path do
      case File.rm(state.socket_path) do
        :ok -> :ok
        {:error, :enoent} -> :ok
        {:error, _} -> :ok
      end
    end

    Logger.debug("MpvPlayer terminate: #{inspect(reason)}")
    :ok
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

            state = %{state | pending: pending}

            if tag == :stop and state.stop_from do
              GenServer.reply(state.stop_from, :ok)
              %{state | stop_from: nil}
            else
              state
            end

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

            state = %{state | pending: pending}

            if tag == :stop and state.stop_from do
              GenServer.reply(state.stop_from, :ok)
              %{state | stop_from: nil}
            else
              state
            end

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

            state = %{state | pending: pending}

            if tag == :stop and state.stop_from do
              GenServer.reply(state.stop_from, {:error, error})
              %{state | stop_from: nil}
            else
              state
            end

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

defmodule HdmiCec do
  require Logger

  @tv_logical_address "0"
  @cec_client_args ["-s", "-d", "1"]

  def available? do
    not is_nil(System.find_executable("cec-client"))
  end

  def probe do
    cond do
      not available?() ->
        Logger.info("hdmi_cec probe: cec-client not installed, using dpms fallback")
        {:ok, :dpms}

      true ->
        case scan() do
          {:ok, output} ->
            if cec_scan_detects_device?(output) do
              Logger.info("hdmi_cec probe: detected responsive cec device(s), using hdmi_cec backend")
              {:ok, :hdmi_cec}
            else
              Logger.info("hdmi_cec probe: no responsive cec device detected, using dpms fallback")
              {:ok, :dpms}
            end

          {:error, reason} ->
            Logger.info("hdmi_cec probe failed #{inspect(reason)}, using dpms fallback")
            {:ok, :dpms}
        end
    end
  end

  def power_on_tv do
    command("on #{@tv_logical_address}")
  end

  def standby_tv do
    command("standby #{@tv_logical_address}")
  end

  def active_source do
    command("as")
  end

  def power_on_and_activate do
    with {:ok, _} <- power_on_tv() do
      Logger.info("hdmi_cec power_on_and_activate: tv power on sent, waiting before active source")
      Process.sleep(1500)
      try_active_source(1)
    end
  end

  def scan do
    command("scan")
  end

  def command(cmd) when is_binary(cmd) do
    case System.find_executable("cec-client") do
      nil ->
        {:error, :cec_client_not_found}

      cec_client ->
        shell_cmd =
          ~s{printf '%s\n' "#{escape_for_double_quotes(cmd)}" | "#{cec_client}" #{Enum.join(@cec_client_args, " ")}}

      case System.cmd("sh", ["-lc", shell_cmd], stderr_to_stdout: true) do
        {output, 0} ->
          trimmed = String.trim(output)

          if cec_output_has_error?(trimmed) do
            {:error, {:cec_command_reported_error, cmd, trimmed}}
          else
            {:ok, trimmed}
          end

        {output, status} ->
          {:error, {:cec_command_failed, cmd, status, String.trim(output)}}
      end
    end
  end

  defp cec_output_has_error?(output) when is_binary(output) do
    text = String.downcase(output)

    String.contains?(text, "cec_transmit failed") or
      String.contains?(text, "ioctl cec_transmit failed") or
      String.contains?(text, "errno=64") or
      String.contains?(text, "failed to") or
      String.contains?(text, "connection closed")
  end

  defp try_active_source(attempt) do
    Logger.info("hdmi_cec active_source attempt=#{attempt}")

    case active_source() do
      {:ok, output} = ok ->
        Logger.info("hdmi_cec active_source attempt=#{attempt} succeeded output=#{inspect(output)}")
        ok

      {:error, reason} ->
        Logger.error("hdmi_cec active_source attempt=#{attempt} failed reason=#{inspect(reason)}; retrying forever")
        Process.sleep(1500)
        try_active_source(attempt + 1)
    end
  end

  defp cec_scan_detects_device?(output) when is_binary(output) do
    text = String.downcase(output)

    cond do
      String.contains?(text, "device #") -> true
      String.contains?(text, "address:") -> true
      String.contains?(text, "vendor:") -> true
      String.contains?(text, "osd string:") -> true
      true -> false
    end
  end

  defp escape_for_double_quotes(value) do
    value
    |> String.replace("\\", "\\\\")
    |> String.replace("\"", "\\\"")
  end
end

defmodule DisplayDpms do
  def force(mode) when mode in [:off, :on] do
    case System.find_executable("xset") do
      nil ->
        {:error, :xset_not_found}

      xset ->
        case System.cmd(xset, ["dpms", "force", Atom.to_string(mode)], stderr_to_stdout: true) do
          {output, 0} ->
            {:ok, String.trim(output)}

          {output, status} ->
            {:error, {:xset_failed, status, String.trim(output)}}
        end
    end
  end
end

defmodule App do
  use GenServer
  require Logger

  @preview_commit_ms 2000
  @resume_rewind_seconds 3
  @screen_wake_delay_ms 5000
  @slideshow_image_display_ms 10_000

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
          Enum.find(channels, fn channel -> channel_id(channel) == saved_current_channel_id end) ||
            List.first(channels)

        init_state = %{
          mediadirs: mediadirs,
          catalog: catalog,
          channels: channels,
          current_channel: current_channel,
          current_channel_id: channel_id(current_channel),
          current_file_index: nil,
          current_player: nil,
          preview_channel: nil,
          preview_timer: nil,
          preview_active?: false,
          screen_off?: false,
          display_backend: :dpms,
          channel_state: saved_channel_state,
          runtime_sup: nil,
          image_display_timer: nil,
          next_prepared: nil,
          next_prepare_task: nil,
          next_prepare_ref: nil,
          next_requested_file_index: nil,
          display_elapsed?: false
        }

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
    {:ok, display_backend} = HdmiCec.probe()

    state =
      state
      |> Map.put(:runtime_sup, runtime_sup)
      |> Map.put(:screen_off?, false)
      |> Map.put(:display_backend, display_backend)
      |> ensure_all_channel_state_entries()

    case display_backend do
      :hdmi_cec ->
        case HdmiCec.power_on_and_activate() do
          {:ok, _} ->
            Logger.info("startup display backend=hdmi_cec; tv on and pi active source")

          {:error, reason} ->
            Logger.error("startup hdmi_cec activation failed #{inspect(reason)}")
        end

      :dpms ->
        case DisplayDpms.force(:on) do
          {:ok, _} ->
            Logger.info("startup display backend=dpms; screen on")

          {:error, reason} ->
            Logger.error("startup dpms activation failed #{inspect(reason)}")
        end
    end

    case state.current_channel do
      nil ->
        {:ok, state}

      channel ->
        {file_index, offset_seconds, state} = load_or_initialize_channel_position(state, channel)

        case play_channel_item(state, channel, file_index, offset_seconds) do
          {:ok, new_state, actual_file_index} ->
            actual_path = Enum.at(channel.media, actual_file_index)
            Logger.info("current channel: #{channel.label}")
            Logger.info("resume position: file=#{inspect(actual_path)} offset=#{offset_seconds}s")
            Osd.show(channel.name)
            {:ok, new_state}

          {:error, new_state, reason} ->
            Logger.error("startup play failed: #{inspect(reason)}")
            {:ok, new_state}
        end
    end
  end

  @impl true
  def handle_info({:input_event, _dev, events}, state) when is_list(events) do
    state =
      if state.screen_off? and wake_keypress?(events) do
        wake_screen(state)
      else
        Enum.reduce(events, state, fn
          {:ev_key, :key_pageup, 1}, state ->
            Logger.info("PgUp -> prev channel")
            preview_prev_channel(state)

          {:ev_key, :key_pagedown, 1}, state ->
            Logger.info("PgDn -> next channel")
            preview_next_channel(state)

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
    current_path = current_original_path(state)

    saved_offset_seconds =
      if PreparedMedia.image_file?(current_path) do
        0
      else
        persisted_resume_offset(seconds)
      end

    state = state |> put_current_playhead(saved_offset_seconds) |> persist_state_file!()
    {:noreply, state}
  end

  def handle_info({:mpv_position, _pid, _seconds}, state), do: {:noreply, state}

  def handle_info({:mpv_end_of_file, pid, reason}, %{current_player: pid} = state) do
    current_path = current_original_path(state)

    Logger.info(
      "mpv_end_of_file: pid=#{inspect(pid)} reason=#{inspect(reason)} current_path=#{inspect(current_path)} image?=#{PreparedMedia.image_file?(current_path)} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared?=#{not is_nil(state.next_prepared)}"
    )

    if PreparedMedia.image_file?(current_path) do
      Logger.debug("mpv_end_of_file: image item reached end-of-file, clearing current_player only")
      {:noreply, %{state | current_player: nil}}
    else
      Logger.info("mpv_end_of_file: advancing non-image item")
      {:noreply, advance_current_channel_item(%{state | current_player: nil})}
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
            advance_to_prepared_next(%{state | current_player: nil})

          _ ->
            Logger.info("mpv process_exit: falling back to normal advance")
            advance_current_channel_item(%{state | current_player: nil})
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
      "image_display_expired: current_path=#{inspect(current_original_path(state))} current_index=#{inspect(state.current_file_index)} next_prepare_ref=#{inspect(state.next_prepare_ref)} next_prepared?=#{not is_nil(state.next_prepared)}"
    )

    state =
      state
      |> Map.put(:image_display_timer, nil)
      |> Map.put(:display_elapsed?, true)

    {:noreply, maybe_advance(state)}
  end

  def handle_info({ref, {:prepared_next_item, file_index, path, result}}, %{next_prepare_ref: ref} = state) do
    expected_path =
      case state.current_channel do
        %{media: media} when is_list(media) and is_integer(file_index) -> Enum.at(media, file_index)
        _ -> nil
      end

    Logger.debug(
      "prepared-next result received ref=#{inspect(ref)} index=#{inspect(file_index)} path=#{inspect(path)} expected_path=#{inspect(expected_path)} path_match?=#{path == expected_path} result=#{inspect(result)}"
    )

    Process.demonitor(ref, [:flush])

    state =
      state
      |> Map.put(:next_prepare_task, nil)
      |> Map.put(:next_prepare_ref, nil)
      |> Map.put(:next_requested_file_index, file_index)
      |> Map.put(:next_prepared, %{file_index: file_index, path: path, result: result})

    {:noreply, maybe_advance(state)}
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, %{next_prepare_ref: ref} = state) do
    Logger.warning(
      "prepared-next task down ref=#{inspect(ref)} requested_index=#{inspect(state.next_requested_file_index)} reason=#{inspect(reason)}"
    )

    state =
      state
      |> Map.put(:next_prepare_task, nil)
      |> Map.put(:next_prepare_ref, nil)
      |> Map.put(:next_prepared, %{
        file_index: state.next_requested_file_index,
        path:
          case state.current_channel do
            %{media: media} when is_list(media) and is_integer(state.next_requested_file_index) ->
              Enum.at(media, state.next_requested_file_index)

            _ ->
              nil
          end,
        result: {:error, {:prepare_task_down, reason}}
      })

    state =
      if PreparedMedia.image_file?(current_original_path(state)) do
        Logger.debug("prepared-next task down while image showing; trying maybe_advance")
        maybe_advance(state)
      else
        state
      end

    {:noreply, state}
  end

  def handle_info(:commit_preview_channel, state) do
    state =
      case state.preview_channel do
        nil ->
          %{state | preview_timer: nil, preview_active?: false}

        preview_channel ->
          if same_channel?(preview_channel, state.current_channel) do
            state
            |> Map.put(:preview_channel, nil)
            |> Map.put(:preview_timer, nil)
            |> Map.put(:preview_active?, false)
          else
            state =
              state
              |> clear_transition_state()
              |> persist_current_channel_position()

            Logger.info("stopping current item before channel commit")
            state = stop_current_item(state)
            {file_index, offset_seconds, state} = load_or_initialize_channel_position(state, preview_channel)

            case play_channel_item(state, preview_channel, file_index, offset_seconds) do
              {:ok, state, actual_file_index} ->
                actual_path = Enum.at(preview_channel.media, actual_file_index)
                Logger.info("committed channel: #{preview_channel.label}")
                Logger.info("resume position: file=#{inspect(actual_path)} offset=#{offset_seconds}s")

                state
                |> Map.put(:preview_channel, nil)
                |> Map.put(:preview_timer, nil)
                |> Map.put(:preview_active?, false)

              {:error, state, reason} ->
                Logger.error("failed to commit #{preview_channel.label}: #{inspect(reason)}")

                state
                |> Map.put(:preview_channel, nil)
                |> Map.put(:preview_timer, nil)
                |> Map.put(:preview_active?, false)
            end
          end
      end

    {:noreply, state}
  end

  def handle_info(:quit, state) do
    Logger.info("Q - quitting")
    Osd.clear()

    state =
      state
      |> clear_transition_state()
      |> persist_current_channel_position()
      |> stop_current_item()
      |> then(fn s -> cancel_preview_timer(s.preview_timer); s end)

    {:stop, :normal, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}

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

  defp wake_keypress?(events) do
    Enum.any?(events, fn
      {:ev_key, _key, 1} -> true
      _ -> false
    end)
  end

  defp toggle_screen(%{screen_off?: true} = state), do: wake_screen(state)
  defp toggle_screen(state), do: turn_screen_off(state)

  defp turn_screen_off(state) do
    Logger.info(
      "turn_screen_off: backend=#{inspect(state.display_backend)} " <>
        "current_channel=#{inspect(state.current_channel && state.current_channel.label)} " <>
        "current_file_index=#{inspect(state.current_file_index)} " <>
        "current_player=#{inspect(state.current_player)} " <>
        "preview_active?=#{inspect(state.preview_active?)} " <>
        "next_prepare_ref=#{inspect(state.next_prepare_ref)} " <>
        "next_prepared?=#{inspect(not is_nil(state.next_prepared))}"
    )

    state =
      state
      |> clear_transition_state()
      |> cancel_active_preview()
      |> persist_current_channel_position()
      |> stop_current_item()

    Process.sleep(150)

    state =
      case state.display_backend do
        :hdmi_cec ->
          case HdmiCec.standby_tv() do
            {:ok, _} ->
              Logger.info("tv standby via hdmi_cec")
              state

            {:error, reason} ->
              Logger.error("hdmi_cec standby failed #{inspect(reason)}; falling back to dpms")

              case DisplayDpms.force(:off) do
                {:ok, _} ->
                  Logger.info("screen off via dpms fallback")

                {:error, dpms_reason} ->
                  Logger.error("dpms fallback off failed #{inspect(dpms_reason)}")
              end

              Map.put(state, :display_backend, :dpms)
          end

        :dpms ->
          case DisplayDpms.force(:off) do
            {:ok, _} ->
              Logger.info("screen off via dpms")

            {:error, reason} ->
              Logger.error("failed to turn screen off via dpms #{inspect(reason)}")
          end

          state
      end

    Map.put(state, :screen_off?, true)
  end

  defp cancel_active_preview(%{preview_active?: false} = state), do: state

  defp cancel_active_preview(state) do
    cancel_preview_timer(state.preview_timer)
    state |> Map.put(:preview_channel, nil) |> Map.put(:preview_timer, nil) |> Map.put(:preview_active?, false)
  end

  defp wake_screen(state) do
    Logger.info(
      "wake_screen: backend=#{inspect(state.display_backend)} " <>
        "current_channel=#{inspect(state.current_channel && state.current_channel.label)} " <>
        "savedcurrentchannelid=#{inspect(state.current_channel_id)} " <>
        "screen_off?=#{inspect(state.screen_off?)}"
    )

    state =
      case state.display_backend do
        :hdmi_cec ->
          case HdmiCec.power_on_and_activate() do
            {:ok, _} ->
              Logger.info("tv powered on and pi restored as active source via hdmi_cec")
              state

            {:error, reason} ->
              Logger.error("hdmi_cec wake failed #{inspect(reason)}; falling back to dpms")

              case DisplayDpms.force(:on) do
                {:ok, _} ->
                  Logger.info("screen on via dpms fallback")

                {:error, dpms_reason} ->
                  Logger.error("dpms fallback on failed #{inspect(dpms_reason)}")
              end

              Map.put(state, :display_backend, :dpms)
          end

        :dpms ->
          case DisplayDpms.force(:on) do
            {:ok, _} ->
              Logger.info("screen on via dpms")

            {:error, reason} ->
              Logger.error("failed to turn screen on via dpms #{inspect(reason)}")
          end

          state
      end

    Process.sleep(@screen_wake_delay_ms)

    state =
      state
      |> clear_transition_state()
      |> Map.put(:screen_off?, false)
      |> Map.put(:preview_channel, nil)
      |> Map.put(:preview_timer, nil)
      |> Map.put(:preview_active?, false)

    case state.current_channel do
      nil ->
        Logger.debug("wake_screen: no current channel to resume")
        state

      channel ->
        {file_index, offset_seconds, state} = load_or_initialize_channel_position(state, channel)

        Logger.info(
          "wake_screen: resuming channel=#{channel.label} " <>
            "requested_file_index=#{inspect(file_index)} offset_seconds=#{inspect(offset_seconds)}"
        )

        case play_channel_item(state, channel, file_index, offset_seconds) do
          {:ok, state, actual_file_index} ->
            actual_path = Enum.at(channel.media, actual_file_index)

            Logger.info(
              "wake_screen: resumed channel=#{channel.label} " <>
                "actual_file_index=#{actual_file_index} actual_path=#{inspect(actual_path)} " <>
                "offset_seconds=#{inspect(offset_seconds)}"
            )

            Osd.show(channel.name)

            state

          {:error, state, reason} ->
            Logger.error("failed to resume after screen wake: #{inspect(reason)}")
            state
        end
    end
  end

  defp preview_next_channel(%{channels: []} = state), do: state

  defp preview_next_channel(%{channels: channels} = state) do
    base_channel = state.preview_channel || state.current_channel
    current_index = current_channel_index(channels, base_channel)
    next_index = rem(current_index + 1, length(channels))
    preview_channel(state, Enum.at(channels, next_index))
  end

  defp preview_prev_channel(%{channels: []} = state), do: state

  defp preview_prev_channel(%{channels: channels} = state) do
    base_channel = state.preview_channel || state.current_channel
    current_index = current_channel_index(channels, base_channel)
    prev_index = rem(current_index - 1 + length(channels), length(channels))
    preview_channel(state, Enum.at(channels, prev_index))
  end

  defp preview_channel(state, channel) do
    cancel_preview_timer(state.preview_timer)
    timer_ref = Process.send_after(self(), :commit_preview_channel, @preview_commit_ms)

    Osd.show(channel.name)

    state
    |> Map.put(:preview_channel, channel)
    |> Map.put(:preview_timer, timer_ref)
    |> Map.put(:preview_active?, true)
  end

  defp maybe_advance(%{current_channel: nil} = state) do
    Logger.debug("maybe_advance: no current channel")
    state
  end

  defp maybe_advance(state) do
    current_path = current_original_path(state)
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

  defp advance_to_prepared_next(%{next_prepared: %{file_index: file_index, result: {:ok, prepared}}} = state) do
    channel = state.current_channel
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "advance_to_prepared_next: channel=#{channel.label} file_index=#{file_index} actual_path=#{inspect(actual_path)} prepared_play_path=#{inspect(prepared.play_path)}"
    )

    state =
      state
      |> clear_transition_state()
      |> stop_current_item()

    case start_current_item(state, channel, file_index, prepared, 0) do
      {:ok, state, _actual_file_index} ->
        Logger.info("advanced #{channel.label} to prepared #{inspect(actual_path)}")
        state

      {:error, reason} ->
        Logger.error("failed starting prepared next item #{inspect(actual_path)}: #{inspect(reason)}")
        advance_current_channel_item(state)
    end
  end

  defp advance_to_prepared_next(%{next_prepared: %{path: path, result: {:error, reason}}} = state) do
    Logger.error("async prepare failed for next item #{inspect(path)}: #{inspect(reason)}")

    state
    |> clear_transition_state()
    |> advance_current_channel_item()
  end

  defp advance_to_prepared_next(state) do
    Logger.debug(
      "advance_to_prepared_next: no usable prepared item next_prepared=#{inspect(state.next_prepared)}"
    )

    state
  end

  defp advance_current_channel_item(%{current_channel: nil} = state) do
    Logger.debug("advance_current_channel_item: skip no current channel")
    state
  end

  defp advance_current_channel_item(state) do
    channel = state.current_channel
    requested_file_index = next_file_index(state.current_file_index, channel.media)

    Logger.info(
      "advance_current_channel_item: channel=#{channel.label} current_index=#{inspect(state.current_file_index)} requested_next_index=#{requested_file_index}"
    )

    state = stop_current_item(state)

    case play_channel_item(state, channel, requested_file_index, 0) do
      {:ok, state, actual_file_index} ->
        actual_path = Enum.at(channel.media, actual_file_index)

        Logger.info(
          "advance_current_channel_item: started channel=#{channel.label} actual_index=#{actual_file_index} actual_path=#{inspect(actual_path)}"
        )

        updated_channel_state =
          Map.put(state.channel_state, state.current_channel_id, %{
            "file_path" => actual_path,
            "offset_seconds" => 0
          })

        state
        |> Map.put(:channel_state, updated_channel_state)
        |> persist_state_file!()

      {:error, state, reason} ->
        Logger.error(
          "advance_current_channel_item: failed channel=#{channel.label} requested_next_index=#{requested_file_index} reason=#{inspect(reason)}"
        )

        %{state | current_player: nil}
    end
  end

  defp play_channel_item(state, channel, file_index, offset_seconds) do
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
            normalized_index = normalize_index(file_index, media)

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

        try_play_channel_item(
          state,
          channel,
          media,
          requested_index,
          offset_seconds,
          next_file_index(candidate_index, media),
          attempts + 1,
          max_attempts
        )

      not File.regular?(path) ->
        Logger.warning("skipping missing file for #{channel.label}: #{inspect(path)}")

        try_play_channel_item(
          state,
          channel,
          media,
          requested_index,
          offset_seconds,
          next_file_index(candidate_index, media),
          attempts + 1,
          max_attempts
        )

      true ->
        actual_offset =
          cond do
            PreparedMedia.image_file?(path) ->
              0

            candidate_index == requested_index ->
              normalize_offset(offset_seconds)

            true ->
              0
          end

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

            try_play_channel_item(
              state,
              channel,
              media,
              requested_index,
              offset_seconds,
              next_file_index(candidate_index, media),
              attempts + 1,
              max_attempts
            )
        end
    end
  end

  defp start_current_item(state, channel, file_index, prepared, startpos) do
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "start_current_item: channel=#{channel.label} index=#{file_index} actual_path=#{inspect(actual_path)} play_path=#{inspect(prepared.play_path)} startpos=#{inspect(startpos)} tmp?=#{inspect(prepared.tmp?)} downscaled?=#{inspect(prepared.downscaled?)}"
    )

    with {:ok, playerpid} <- MpvPlayer.start_link(parent: self(), file: prepared.play_path, start_pos: startpos) do
      updated_channel_state =
        Map.put(state.channel_state, channel_id(channel), %{
          file_path: actual_path,
          offset_seconds: 0
        })

      new_state =
        state
        |> clear_transition_state()
        |> Map.put(:current_channel, channel)
        |> Map.put(:current_channel_id, channel_id(channel))
        |> Map.put(:current_file_index, file_index)
        |> Map.put(:current_player, playerpid)
        |> Map.put(:channel_state, updated_channel_state)
        |> put_current_playhead(0)
        |> persist_state_file!()
        |> maybe_start_image_display_timer(actual_path)
        |> maybe_prepare_next_item()

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

  defp stop_current_item(%{current_player: nil} = state) do
    Logger.debug("stop_current_item: no current player")
    state
  end

  defp stop_current_item(state) do
    Logger.info(
      "stop_current_item: player=#{inspect(state.current_player)} current_path=#{inspect(current_original_path(state))}"
    )

    try do
      MpvPlayer.stop(state.current_player)
      Logger.debug("stop_current_item: stop request completed")
    catch
      :exit, reason ->
        Logger.warning("stop_current_item: stop raised exit reason=#{inspect(reason)}")
        :ok
    end

    Map.put(state, :current_player, nil)
  end

  defp maybe_start_image_display_timer(state, path) do
    cancel_image_display_timer(state.image_display_timer)

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

  defp maybe_prepare_next_item(%{current_channel: nil} = state) do
    Logger.debug("maybe_prepare_next_item: skip no current channel")
    state
  end

  defp maybe_prepare_next_item(%{screen_off?: true} = state) do
    Logger.debug("maybe_prepare_next_item: skip screen off")
    state
  end

  defp maybe_prepare_next_item(%{next_prepare_task: task} = state) when not is_nil(task) do
    Logger.debug("maybe_prepare_next_item: skip task already running")
    state
  end

  defp maybe_prepare_next_item(%{next_prepared: prepared} = state) when not is_nil(prepared) do
    Logger.debug("maybe_prepare_next_item: skip next item already prepared")
    state
  end

  defp maybe_prepare_next_item(state) do
    media = state.current_channel.media

    if media == [] or not is_integer(state.current_file_index) do
      Logger.debug(
        "maybe_prepare_next_item: skip media_empty?=#{inspect(media == [])} current_file_index=#{inspect(state.current_file_index)}"
      )

      state
    else
      next_index = next_file_index(state.current_file_index, media)
      next_path = Enum.at(media, next_index)
      owner = self()
      ref = make_ref()

      Logger.debug(
        "maybe_prepare_next_item: starting async prepare next_index=#{next_index} next_path=#{inspect(next_path)} current_index=#{inspect(state.current_file_index)} current_path=#{inspect(current_original_path(state))}"
      )

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

  defp clear_transition_state(state) do
    Logger.debug(
      "clear_transition_state: clearing image timer and prepared-next state current_path=#{inspect(current_original_path(state))}"
    )

    state
    |> cancel_image_timer_and_reset_gate()
    |> clear_next_prepare()
  end

  defp clear_next_prepare(state) do
    if state.next_prepare_task do
      Logger.debug(
        "clear_next_prepare: shutting down task ref=#{inspect(state.next_prepare_ref)} requested_index=#{inspect(state.next_requested_file_index)}"
      )

      Task.shutdown(state.next_prepare_task, :brutal_kill)
    else
      Logger.debug("clear_next_prepare: nothing to clear")
    end

    state
    |> Map.put(:next_prepared, nil)
    |> Map.put(:next_prepare_task, nil)
    |> Map.put(:next_prepare_ref, nil)
    |> Map.put(:next_requested_file_index, nil)
  end

  defp cancel_image_timer_and_reset_gate(state) do
    cancel_image_display_timer(state.image_display_timer)

    state
    |> Map.put(:image_display_timer, nil)
    |> Map.put(:display_elapsed?, false)
  end

  defp cancel_image_display_timer(nil), do: :ok

  defp cancel_image_display_timer(timer_ref) do
    result = Process.cancel_timer(timer_ref)

    Logger.debug(
      "cancel_image_display_timer: timer_ref=#{inspect(timer_ref)} cancel_result=#{inspect(result)}"
    )

    result
  end

  defp persist_current_channel_position(%{current_channel: nil} = state), do: persist_state_file!(state)
  defp persist_current_channel_position(%{current_channel_id: nil} = state), do: persist_state_file!(state)
  defp persist_current_channel_position(%{current_file_index: idx} = state) when not is_integer(idx), do: persist_state_file!(state)

  defp persist_current_channel_position(state), do: persist_state_file!(state)

  defp put_current_playhead(%{current_channel: nil} = state, _offset_seconds), do: state
  defp put_current_playhead(%{current_channel_id: nil} = state, _offset_seconds), do: state
  defp put_current_playhead(%{current_file_index: idx} = state, _offset_seconds) when not is_integer(idx), do: state

  defp put_current_playhead(state, offset_seconds) do
    current_path = current_original_path(state)
    normalized_offset =
      if PreparedMedia.image_file?(current_path), do: 0, else: normalize_offset(offset_seconds)

    Logger.debug(
      "put_current_playhead: channel_id=#{inspect(state.current_channel_id)} file_index=#{inspect(state.current_file_index)} path=#{inspect(current_path)} offset_seconds=#{normalized_offset}"
    )

    updated_channel_state =
      Map.put(state.channel_state, state.current_channel_id, %{
        "file_path" => current_path,
        "offset_seconds" => normalized_offset
      })

    %{state | channel_state: updated_channel_state}
  end

  defp ensure_all_channel_state_entries(state) do
    channel_state =
      Enum.reduce(state.channels, state.channel_state, fn channel, acc ->
        Map.put_new(acc, channel_id(channel), %{"file_path" => nil, "offset_seconds" => 0})
      end)

    %{state | channel_state: channel_state}
  end

  defp load_or_initialize_channel_position(state, channel) do
    id = channel_id(channel)

    case Map.fetch(state.channel_state, id) do
      {:ok, %{"file_path" => saved_file_path, "offset_seconds" => offset_seconds}} ->
        file_index = resolve_file_index(channel.media, saved_file_path)
        restored_offset = restore_offset_for_channel(channel, file_index, offset_seconds)
        {file_index, restored_offset, state}

      :error ->
        updated_channel_state = Map.put(state.channel_state, id, %{"file_path" => nil, "offset_seconds" => 0})
        {0, 0, %{state | channel_state: updated_channel_state}}
    end
  end

  defp restore_offset_for_channel(channel, file_index, saved_offset_seconds) do
    path = Enum.at(channel.media, file_index)
    if PreparedMedia.image_file?(path), do: 0, else: normalize_offset(saved_offset_seconds)
  end

  defp resolve_file_index(media, saved_file_path) when is_binary(saved_file_path),
    do: Enum.find_index(media, &(&1 == saved_file_path)) || 0

  defp resolve_file_index(_media, _saved_file_path), do: 0

  defp persisted_resume_offset(offset_seconds),
    do: max(normalize_offset(offset_seconds) - @resume_rewind_seconds, 0)

  defp persist_state_file!(state) do
    persisted = %{"version" => 1, "current_channel_id" => state.current_channel_id, "channel_state" => state.channel_state}
    :ok = StateStore.save!(persisted)
    state
  end

  defp current_original_path(%{current_channel: nil}), do: nil
  defp current_original_path(%{current_file_index: idx}) when not is_integer(idx), do: nil
  defp current_original_path(state), do: Enum.at(state.current_channel.media, state.current_file_index)

  defp channel_id(channel), do: channel.path

  defp normalize_index(value, media) when is_integer(value) do
    count = length(media)

    cond do
      count <= 0 -> 0
      value >= 0 -> rem(value, count)
      true -> 0
    end
  end

  defp normalize_index(_value, _media), do: 0

  defp next_file_index(current_index, media) when is_list(media) and media != [] do
    rem(normalize_index(current_index, media) + 1, length(media))
  end

  defp next_file_index(_current_index, _media), do: 0

  defp normalize_offset(value) when is_integer(value) and value >= 0, do: value
  defp normalize_offset(value) when is_float(value) and value >= 0, do: trunc(value)
  defp normalize_offset(_), do: 0

  defp current_channel_index(channels, channel) do
    Enum.find_index(channels, &same_channel?(&1, channel)) || 0
  end

  defp same_channel?(nil, nil), do: true
  defp same_channel?(nil, _), do: false
  defp same_channel?(_, nil), do: false
  defp same_channel?(a, b), do: channel_id(a) == channel_id(b)

  defp cancel_preview_timer(nil), do: :ok
  defp cancel_preview_timer(timer_ref), do: Process.cancel_timer(timer_ref)

  defp usage(message) do
    IO.puts(message)
    IO.puts("usage: #{Script.name()} [--scan] MEDIA_DIR [MEDIA_DIR ...]")
    System.halt(1)
  end
end

App.main(System.argv())
