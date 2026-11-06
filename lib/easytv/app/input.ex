defmodule App.Input do
  use GenServer
  require Logger

  @preview_commit_ms 2000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, opts)
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
  def handle_cast({:control, action}, state) do
    {:noreply, apply_control(action, state)}
  end

  @impl true
  def handle_info(
        {:commit_preview, seq},
        %{preview_session: %PreviewSession{seq: active_seq}} = state
      )
      when seq != active_seq do
    Logger.debug(
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
    Logger.debug(
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
    Logger.debug(
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
    Logger.debug(
      "commit_preview handling " <>
        "seq=#{inspect(seq)} " <>
        "current=#{inspect(state.current_channel && state.current_channel.name)} " <>
        "target=#{inspect(session.target_channel && session.target_channel.name)} " <>
        "wrapped_to_current?=#{inspect(session.wrapped_to_current?)}"
    )

    if App.Util.same_channel?(session.target_channel, state.current_channel) do
      Logger.debug(
        "commit_preview cleared: preview_equals_current " <>
          "channel=#{inspect(state.current_channel && state.current_channel.name)} " <>
          "seq=#{inspect(seq)}"
      )

      {:noreply, clear_preview_session(state)}
    else
      Logger.debug(
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
    Logger.debug(
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

    state = put_in(state.preview_session.switching?, false)

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
  def handle_info(_msg, state), do: {:noreply, state}

  defp apply_control(:channel_previous, state) do
    preview_prev_channel(state)
  end

  defp apply_control(:channel_next, state) do
    preview_next_channel(state)
  end

  defp apply_control(action, %{playback_owner: nil} = state)
       when action in [:stop, :resume] do
    Logger.error("control action=#{action} failed: playback owner is not running")
    state
  end

  defp apply_control(:stop, state) do
    state = clear_preview_session(state)
    Osd.clear()
    GenServer.cast(state.playback_owner, :stop)
    state
  end

  defp apply_control(:resume, state) do
    GenServer.cast(state.playback_owner, :resume)
    state
  end

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

    Logger.debug(
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

    Logger.debug(
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

    Logger.debug(
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
