defmodule EasyTV.UserConfig do
  require Logger

  def config do
    [
      state: %{focus: :unknown, adapter: nil},
      on_key: &on_key/2,
      on_cec: &on_cec/2,
      on_mpv: &on_mpv/2
    ]
  end

  def on_key({:ev_key, key, 1}, %{focus: focus} = state)
      when key in [:key_pageup, :key_pagedown] and focus in [:away, :standby] do
    Logger.info("Wake up and resume playback on persisted channel")
    EasyTV.Controls.resume()
    assert_active_source()
    %{state | focus: :return_requested}
  end

  def on_key({:ev_key, :key_pageup, 1}, state) do
    Logger.info("Previous channel")
    EasyTV.Controls.channel_previous()
    assert_active_source()
    state
  end

  def on_key({:ev_key, :key_pagedown, 1}, state) do
    Logger.info("Next channel")
    EasyTV.Controls.channel_next()
    assert_active_source()
    state
  end

  def on_key(event, state) do
    Logger.debug("Unhandled key event=#{inspect(event)} state=#{inspect(state)}")
    state
  end

  def on_cec(%{type: :focus_lost, adapter: adapter}, state) do
    Logger.info("Another device became Active Source; stop playback")
    EasyTV.Controls.stop()
    %{state | focus: :away, adapter: adapter}
  end

  def on_cec(%{type: :tv_standby}, state) do
    Logger.info("TV standby; stop playback")
    EasyTV.Controls.stop()
    %{state | focus: :standby}
  end

  def on_cec(%{type: :tv_selected, adapter: adapter}, state) do
    if state.focus not in [:here, :return_requested] or state.adapter != adapter do
      Logger.info("TV selected this device; resume current channel")
      EasyTV.Controls.resume()
    end

    %{state | focus: :here, adapter: adapter}
  end

  def on_cec(%{type: :focus_here, adapter: adapter}, state) do
    %{state | focus: :here, adapter: adapter}
  end

  def on_cec(%{type: :observation_gap} = event, state) do
    Logger.warning("CEC focus unknown after gap: #{inspect(event)}")
    %{state | focus: :unknown, adapter: nil}
  end

  def on_cec(event, state) do
    Logger.debug("CEC event=#{inspect(event)} state=#{inspect(state)}")
    state
  end

  defp assert_active_source do
    if EasyTV.Controls.cec_enabled?() do
      EasyTV.Controls.send_cec_command(%{command: "active_source"})
    end
  end

  def on_mpv(%{result: {:error, reason}} = event, state) do
    Logger.error("mpv command failed reason=#{inspect(reason)} event=#{inspect(event)}")
    state
  end

  def on_mpv(event, state) do
    Logger.debug("mpv event=#{inspect(event)} state=#{inspect(state)}")
    state
  end
end
