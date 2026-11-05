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
