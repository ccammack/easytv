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
