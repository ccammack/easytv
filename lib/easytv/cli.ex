defmodule EasyTV.CLI do
  def main(argv) do
    {:ok, _applications} = Application.ensure_all_started(:easytv)
    App.main(argv)
  end
end
