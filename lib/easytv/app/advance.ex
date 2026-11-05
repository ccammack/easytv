defmodule App.Advance do
  require Logger

  @slideshow_image_display_ms 10_000

  def maybe_advance(%{current_channel: nil} = state) do
    Logger.debug("maybe_advance no current channel")
    state
  end

  def maybe_advance(state) do
    current_path = App.Util.current_original_path(state)
    image? = PreparedMedia.image_file?(current_path)

    Logger.debug(
      "maybe_advance current_path=#{inspect(current_path)} image?=#{inspect(image?)} display_elapsed?=#{inspect(state.display_elapsed?)} next_prepared=#{inspect(Map.take(state.next_prepared || %{}, [:file_index, :path, :result]))}"
    )

    cond do
      image? and state.display_elapsed? and not is_nil(state.next_prepared) ->
        Logger.info("maybe_advance conditions met, advancing to prepared next item")
        advance_to_prepared_next(state)

      true ->
        Logger.debug("maybe_advance conditions not met, waiting")
        state
    end
  end

  def advance_to_prepared_next(
        %{next_prepared: %{file_index: file_index, result: {:ok, prepared}}} = state
      ) do
    channel = state.current_channel
    actual_path = Enum.at(channel.media, file_index)

    Logger.info(
      "advance_to_prepared_next channel=#{channel.label} file_index=#{file_index} actual_path=#{inspect(actual_path)} prepared_playpath=#{inspect(prepared.play_path)}"
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
          "failed starting prepared next item #{inspect(actual_path)} #{inspect(reason)}"
        )

        advance_current_channel_item(state)
    end
  end

  def advance_to_prepared_next(%{next_prepared: %{path: path, result: {:error, reason}}} = state) do
    Logger.error("async prepare failed for next item #{inspect(path)} #{inspect(reason)}")

    state
    |> clear_transition_state()
    |> advance_current_channel_item()
  end

  def advance_to_prepared_next(state) do
    Logger.debug(
      "advance_to_prepared_next no usable prepared item next_prepared=#{inspect(state.next_prepared)}"
    )

    state
  end

  def advance_current_channel_item(%{current_channel: nil} = state) do
    Logger.debug("advance_current_channel_item skip no current channel")
    state
  end

  def advance_current_channel_item(%{switching?: true} = state) do
    Logger.info(
      "advance_current_channel_item skipping because channel switch is in progress current_channel=#{inspect(state.current_channel && state.current_channel.label)} current_index=#{inspect(state.current_file_index)} switching_seq=#{inspect(state.switching_seq)}"
    )

    %{state | current_player: nil}
  end

  def advance_current_channel_item(state) do
    channel = state.current_channel
    requested_file_index = App.Util.next_file_index(state.current_file_index, channel.media)

    Logger.info(
      "advance_current_channel_item channel=#{channel.label} current_index=#{inspect(state.current_file_index)} requested_next_index=#{requested_file_index} current_player=#{inspect(state.current_player)}"
    )

    state = App.Playback.stop_current_item(state)

    case App.Playback.play_channel_item(state, channel, requested_file_index, 0) do
      {:ok, state, actual_file_index} ->
        actual_path = Enum.at(channel.media, actual_file_index)

        Logger.info(
          "advance_current_channel_item started #{channel.label} actual_index=#{actual_file_index} actual_path=#{inspect(actual_path)}"
        )

        state
        |> App.Persistence.put_current_playhead(0)
        |> App.Persistence.persist_state_file!()

      {:error, state, reason} ->
        Logger.error(
          "advance_current_channel_item failed channel=#{channel.label} requested_next_index=#{requested_file_index} reason=#{inspect(reason)}"
        )

        %{state | current_player: nil}
    end
  end

  def maybe_start_image_display_timer(state, path) do
    App.Util.cancel_image_display_timer(state.image_display_timer)

    if PreparedMedia.image_file?(path) do
      timer_ref = Process.send_after(self(), :image_display_expired, @slideshow_image_display_ms)

      Logger.debug(
        "maybe_start_image_display_timer armed path=#{inspect(path)} timer_ref=#{inspect(timer_ref)} display_ms=#{@slideshow_image_display_ms}"
      )

      %{state | image_display_timer: timer_ref, display_elapsed?: false}
    else
      Logger.debug("maybe_start_image_display_timer skipped non-image path=#{inspect(path)}")
      %{state | image_display_timer: nil, display_elapsed?: false}
    end
  end

  def maybe_prepare_next_item(%{current_channel: nil} = state) do
    Logger.debug("maybe_prepare_next_item skip no current channel")
    state
  end

  def maybe_prepare_next_item(%{next_prepare_task: task} = state) when not is_nil(task) do
    Logger.debug("maybe_prepare_next_item skip task already running")
    state
  end

  def maybe_prepare_next_item(%{next_prepared: prepared} = state) when not is_nil(prepared) do
    Logger.debug("maybe_prepare_next_item skip next item already prepared")
    state
  end

  def maybe_prepare_next_item(state) do
    media = state.current_channel.media

    if media == [] or not is_integer(state.current_file_index) do
      Logger.debug(
        "maybe_prepare_next_item skip media_empty?=#{inspect(media == [])} current_file_index=#{inspect(state.current_file_index)}"
      )

      state
    else
      next_index = App.Util.next_file_index(state.current_file_index, media)
      next_path = Enum.at(media, next_index)

      Logger.debug(
        "maybe_prepare_next_item starting async prepare next_index=#{next_index} next_path=#{inspect(next_path)} current_index=#{inspect(state.current_file_index)} current_path=#{inspect(App.Util.current_original_path(state))}"
      )

      start_prepare_next_task(state, next_index, next_path)
    end
  end

  def clear_transition_state(state) do
    Logger.debug(
      "clear_transition_state clearing image timer and prepared-next state current_path=#{inspect(App.Util.current_original_path(state))}"
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

  def clear_next_prepare(state) do
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
        result = PreparedMedia.prepare_for_playback(next_path)
        elapsed_ms = System.monotonic_time(:millisecond) - started_ms

        Logger.debug(
          "maybe_prepare_next_item finished async prepare next_index=#{next_index} next_path=#{inspect(next_path)} elapsed_ms=#{elapsed_ms} result=#{inspect(result)}"
        )

        send(owner, {ref, {:prepared_next_item, next_index, next_path, result}})
      end)

    state
    |> Map.put(:next_prepare_task, task)
    |> Map.put(:next_prepare_ref, ref)
    |> Map.put(:next_requested_file_index, next_index)
  end
end
