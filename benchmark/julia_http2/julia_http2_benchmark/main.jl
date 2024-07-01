using CloudStore, JSON, Dates

struct Input
    Bucket::String
    ObjectsPath::String
end

function Input(d::Dict)
    return Input(d["Bucket"], d["ObjectsPath"])
end

@kwdef struct Output
    DtMs::Float64
end

function do_benchmark(download_strategy, objects)
    tstart = now(UTC)
    download_strategy(objects)
    tend = now(UTC)
    return Output(; DtMs = Millisecond(tend - tstart).value)
end

function main()
    println(ARGS[1])
    input = Input(JSON.parse(ARGS[1]))

    credentials = CloudStore.AWS.Credentials()
    b = CloudStore.AWS.Bucket(input.Bucket, "us-east-2") # TODO dont hardcode region
    objects = JSON.parsefile(input.ObjectsPath)

    Base.CoreLogging.with_logstate(Base.CoreLogging.LogState(Base.CoreLogging.Error, Base.current_logger())) do
        # warmup
        n = min(Threads.nthreads() * 2, length(objects))
        do_benchmark(objects[1:n]) do objects
            Threads.@threads for object in objects
                for _ = 1:3
                    try
                        data = CloudStore.get(b, object; credentials)
                        break
                    catch ex
                        @error ex
                    end
                end
            end
        end

        # run
        output = do_benchmark(objects) do objects
            Threads.@threads for object in objects
                for _ = 1:3
                    try
                        data = CloudStore.get(b, object; credentials)
                        break
                    catch ex
                        @error ex
                    end
                end
            end
        end
        println(json(output))
    end
end

main()
