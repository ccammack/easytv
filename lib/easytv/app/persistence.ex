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
