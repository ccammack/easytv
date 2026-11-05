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

    Logger.debug(
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
  def handle_cast({:user_command, ref, command}, state) do
    if state.stopping? do
      send_event(state, :command_error, %{tag: {:user, ref}, error: :player_stopping})
      {:noreply, state}
    else
      {_result, state} = send_command(state, command, {:user, ref})
      {:noreply, state}
    end
  end

  def handle_cast(:stop, %{stopping?: true} = state) do
    Logger.debug(
      "mpv stop: already stopping wrapper_pid=#{inspect(self())} port=#{inspect(state.port)} os_pid=#{inspect(state.port_os_pid)} file=#{inspect(state.file)}"
    )

    {:noreply, state}
  end

  def handle_cast(:stop, state) do
    state = %{state | stopping?: true}

    Logger.debug(
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
    if status != 0 and not state.stopping? do
      Logger.warning("mpv exited unexpectedly: status=#{status} file=#{inspect(state.file)}")
    end

    Logger.debug(
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
    if reason not in [:normal, :shutdown] do
      Logger.warning("mpv wrapper terminated: #{inspect(reason)} file=#{inspect(state.file)}")
    end

    Logger.debug(
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
    Logger.debug(fn ->
      try do
        {output, status} =
          System.cmd(
            "ps",
            ["-o", "pid,ppid,pgid,stat,etime,command=", "-p", Integer.to_string(os_pid)],
            stderr_to_stdout: true
          )

        "#{label}: os_pid=#{os_pid} ps_status=#{status} ps_output=#{inspect(String.trim(output))}"
      catch
        kind, reason ->
          "#{label}: os_pid=#{os_pid} ps raised #{inspect({kind, reason})}"
      end
    end)
  end

  defp socket_path do
    Script.tmp_file("mpv-#{:erlang.unique_integer([:positive, :monotonic])}.sock")
    |> Script.validate_socket_path!()
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
    Logger.debug(
      "force_kill_port_os_pid: wrapper_pid=#{inspect(self())} port=#{inspect(port)} os_pid=#{inspect(os_pid)} file=#{inspect(state.file)}"
    )

    log_process_snapshot("force_kill pre", os_pid)

    try do
      {kill_output, kill_status} =
        System.cmd("kill", ["-KILL", Integer.to_string(os_pid)], stderr_to_stdout: true)

      Logger.debug(
        "force_kill_port_os_pid: kill pid=#{os_pid} status=#{kill_status} output=#{inspect(kill_output)}"
      )
    catch
      kind, reason ->
        Logger.warning("force_kill_port_os_pid: kill raised #{inspect({kind, reason})}")
    end

    log_process_snapshot("force_kill post", os_pid)

    if port do
      try do
        Logger.debug("force_kill_port_os_pid: closing port #{inspect(port)}")
        Port.close(port)
      catch
        :error, :badarg ->
          Logger.debug("force_kill_port_os_pid: port already closed #{inspect(port)}")

        kind, reason ->
          Logger.warning("force_kill_port_os_pid: Port.close raised #{inspect({kind, reason})}")
      end
    end

    :ok
  end
end
