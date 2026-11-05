defmodule App.Playback do
  require Logger

  def pause_playback(state) do
    Logger.debug(
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
    Logger.debug(
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

        Logger.debug(
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

    Logger.debug(
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
    state = App.PlaybackOwner.cancel_player_requests(state, player)

    Logger.debug(
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
        Logger.debug("stop_current_item: stop dispatched player=#{inspect(player)}")

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
