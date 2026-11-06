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
              switching_seq: nil,
              user_requests: %{}
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
  def handle_call(:current_media, _from, %{current_player: nil} = state) do
    Logger.warning("current media query failed: no active player")
    {:reply, {:error, :no_player}, state}
  end

  def handle_call(:current_media, _from, state) do
    path = App.Util.current_original_path(state)
    media = %{path: path, kind: MediaTypes.kind(path), playback_id: state.current_player}
    {:reply, {:ok, media}, state}
  end

  @impl true
  def handle_cast({:mpv_command, ref, command}, %{current_player: nil} = state) do
    report_mpv_result(ref, command, {:error, :no_player})
    {:noreply, state}
  end

  def handle_cast({:mpv_command, ref, command}, state) do
    player = state.current_player
    monitor = Process.monitor(player)
    timer = Process.send_after(self(), {:user_command_timeout, ref}, 4000)
    GenServer.cast(player, {:user_command, ref, command})
    request = %{player: player, monitor: monitor, timer: timer, command: command}
    {:noreply, %{state | user_requests: Map.put(state.user_requests, ref, request)}}
  end

  def handle_cast({:begin_switch, seq, channel}, state) do
    Logger.debug("begin_switch seq=#{inspect(seq)} channel=#{inspect(channel && channel.name)}")

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
  def handle_cast(:stop, state) do
    state = App.Playback.pause_playback(state)
    {:noreply, %{state | switching?: false, switching_seq: nil}}
  end

  def handle_cast(:resume, %{current_player: nil} = state) do
    state = App.Playback.resume_playback(state)

    if state.current_player do
      send(state.input_pid, {:playback_started, state.current_channel})
    end

    {:noreply, state}
  end

  def handle_cast(:resume, state), do: {:noreply, state}

  @impl true
  def handle_info({:mpv_event, pid, kind, %{tag: {:user, ref}} = payload}, state) do
    result =
      cond do
        pid != state.current_player ->
          {:error, :player_replaced}

        kind == :reply ->
          {:ok, Map.get(payload, :reply, %{"error" => "success", "data" => payload[:data]})}

        kind == :request_timeout ->
          {:error, :timeout}

        true ->
          {:error, Map.get(payload, :error, Map.get(payload, :reason, :ipc_error))}
      end

    {:noreply, finish_user_request(state, ref, result)}
  end

  def handle_info({:user_command_timeout, ref}, state) do
    {:noreply, finish_user_request(state, ref, {:error, :timeout})}
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

  def handle_info(
        {:mpv_end_of_file, pid, reason},
        %{current_player: pid, switching?: true} = state
      ) do
    state = cancel_player_requests(state, pid)

    Logger.debug(
      "mpv_end_of_file ignoring current-player eof during switch pid=#{inspect(pid)} reason=#{inspect(reason)}"
    )

    {:noreply, %{state | current_player: nil}}
  end

  def handle_info({:mpv_end_of_file, pid, reason}, %{current_player: pid} = state) do
    state = cancel_player_requests(state, pid)
    current_path = App.Util.current_original_path(state)

    Logger.debug(
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
    state = cancel_player_requests(state, pid, :player_exited)
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

  def handle_info({:DOWN, monitor, :process, _pid, _reason}, state) do
    case Enum.find(state.user_requests, fn {_ref, request} -> request.monitor == monitor end) do
      {ref, _request} ->
        {:noreply, finish_user_request(state, ref, {:error, :player_exited})}

      nil ->
        {:noreply, state}
    end
  end

  def handle_info(_msg, state), do: {:noreply, state}

  @impl true
  def terminate(reason, state) do
    Logger.info("playback owner shutting down: #{inspect(reason)}")
    player = state.current_player
    player_ref = if player, do: Process.monitor(player)
    task = state.next_prepare_task
    App.Playback.pause_playback(state)

    if task, do: Task.shutdown(task, 1000)

    if player_ref do
      receive do
        {:DOWN, ^player_ref, :process, ^player, reason}
        when reason in [:normal, :shutdown, :noproc] ->
          :ok

        {:DOWN, ^player_ref, :process, ^player, reason} ->
          Logger.error("player shutdown failed: #{inspect(reason)}")
      after
        5000 ->
          Process.demonitor(player_ref, [:flush])
          Logger.error("player shutdown timed out: #{inspect(player)}")
      end
    end
  end

  def cancel_player_requests(state, player, reason \\ :player_replaced) do
    Enum.reduce(state.user_requests, state, fn
      {ref, %{player: ^player}}, acc -> finish_user_request(acc, ref, {:error, reason})
      _, acc -> acc
    end)
  end

  defp finish_user_request(state, ref, result) do
    case Map.pop(state.user_requests, ref) do
      {nil, _requests} ->
        state

      {request, requests} ->
        Process.cancel_timer(request.timer)
        Process.demonitor(request.monitor, [:flush])
        report_mpv_result(ref, request.command, result)
        %{state | user_requests: requests}
    end
  end

  defp report_mpv_result(ref, command, result) do
    case result do
      {:error, reason} -> Logger.error("user mpv command failed: #{inspect(reason)}")
      _ -> :ok
    end

    EasyTV.ConfigDispatcher.mpv_event(%{type: :reply, ref: ref, command: command, result: result})
  end

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
