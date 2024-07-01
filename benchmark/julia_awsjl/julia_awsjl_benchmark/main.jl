using AWS, JSON, Random, Dates, SHA, Profile
@service S3

@kwdef struct Output
    DtMs::Float64
    Profile::String
end

function download_parts(key, aws_config, input)
    content_length_header = input.Backend == "http" ? "Content-Length" : "content-length"
    response = S3.head_object(input.Bucket, key; aws_config)
    object_length = parse(Int, response[content_length_header])
    if object_length < input.DownloadPartSizeBytes
        return S3.get_object(input.Bucket, key, Dict("response-content-type" => "application/octet-stream"); aws_config)
    end

    buf = Vector{UInt8}(undef, object_length)
    sem = Base.Semaphore(input.DownloadPartsNThreads)
    next_byte_range_start = 0
    @sync while next_byte_range_start < object_length
        range_end = next_byte_range_start + input.DownloadPartSizeBytes
        if range_end >= object_length
            range_end = object_length - 1
        end
        this_thread_range_start = next_byte_range_start
        next_byte_range_start = range_end + 1

        Base.acquire(sem)
        t = Threads.@spawn begin
            try
                obj_buf = S3.get_object(
                    input.Bucket,
                    key,
                    Dict(
                        "response-content-type" => "application/octet-stream",
                        "headers" => Dict("Range" => "bytes=$this_thread_range_start-$range_end"),
                    );
                    aws_config,
                )
                buf[(this_thread_range_start+1):(range_end+1)] .= obj_buf
            finally
                Base.release(sem)
            end
        end
        Base.errormonitor(t)
    end
    return buf
end

struct Input
    Backend::String
    Bucket::String
    ObjectsPath::String
    WriteToDisk::Bool
    DownloadStrategy::String
    DownloadPartSizeBytes::Int
    DownloadPartsNThreads::Int
    ShouldProfile::Bool
end

function Input(d::Dict)
    return Input(
        d["Backend"],
        d["Bucket"],
        d["ObjectsPath"],
        d["WriteToDisk"],
        d["DownloadStrategy"],
        d["DownloadPartSizeBytes"],
        d["DownloadPartsNThreads"],
        d["ShouldProfile"],
    )
end

function download_objects_dynamic_threads_no_parts(objects, aws_config, dir, input)
    Threads.@threads for object in objects
        data = S3.get_object(input.Bucket, object; aws_config)
        if input.WriteToDisk
            path = joinpath(dir, input.Bucket, object)
            mkpath(dirname(path))
            open(path, "w+") do f
                write(f, data)
            end
        end
    end
end

function download_objects_dynamic_threads_parts(objects, aws_config, dir, input)
    Threads.@threads for object in objects
        data = download_parts(object, aws_config, input)
        if input.WriteToDisk
            path = joinpath(dir, input.Bucket, object)
            mkpath(dirname(path))
            open(path, "w+") do f
                write(f, data)
            end
        end
    end
end

@static if Int(VERSION.minor) >= 11
    function download_objects_greedy_threads_parts(objects, aws_config, dir, input)
        Threads.@threads :greedy for object in objects
            data = download_parts(object, aws_config, input)
            if input.WriteToDisk
                path = joinpath(dir, input.Bucket, object)
                mkpath(dirname(path))
                open(path, "w+") do f
                    write(f, data)
                end
            end
        end
    end
end

function download_objects_keep_off_thread_1_no_parts(objects, aws_config, dir, input)
    available_threads = Ref(collect(2:Threads.nthreads()))
    sem = Base.Semaphore(length(available_threads))
    l = ReentrantLock()
    ts = []
    for object in objects
        Base.acquire(sem)
        tid = lock(l) do
            tid = available_threads[][1]
            available_threads[] = available_threads[][2:end]
            return tid
        end
        t = Task() do
            try
                data = S3.get_object(input.Bucket, object; aws_config)
                if input.WriteToDisk
                    path = joinpath(dir, input.Bucket, object)
                    mkpath(dirname(path))
                    open(path, "w+") do f
                        write(f, data)
                    end
                end
            finally
                Base.release(sem)
                lock(l) do
                    push!(available_threads[], tid)
                end
            end
        end
        t.sticky = true
        ccall(:jl_set_task_tid, Cvoid, (Any, Cint), t, tid - 1)
        schedule(t)
        push!(ts, t)
    end
    for t in ts
        wait(t)
    end
end

function do_benchmark(objects, input, download_strategy)
    tstart = now(UTC)
    if input.ShouldProfile
        Profile.@profile download_strategy(objects)
    else
        download_strategy(objects)
    end
    tend = now(UTC)
    profile_str = if input.ShouldProfile
        iob = IOBuffer()
        Profile.print(IOContext(iob, :displaysize => (24, 2000)); C = true, combine = true, mincount = 10)
        String(take!(iob))
    else
        ""
    end
    return Output(; DtMs = Millisecond(tend - tstart).value, Profile = profile_str)
end

function main()
    println(ARGS[1])
    input = Input(JSON.parse(ARGS[1]))

    AWS.DEFAULT_BACKEND[] = input.Backend == "http" ? AWS.HTTPBackend() : AWS.DownloadsBackend()
    aws_config = AWS.AWSConfig()
    dir = mkpath(joinpath(homedir(), randstring(8)))

    download_strategy = if input.DownloadStrategy == "dynamic threads, no parts"
        objects -> download_objects_dynamic_threads_no_parts(objects, aws_config, dir, input)
    elseif input.DownloadStrategy == "dynamic threads, parts"
        objects -> download_objects_dynamic_threads_parts(objects, aws_config, dir, input)
    elseif input.DownloadStrategy == "greedy threads, parts"
        objects -> download_objects_greedy_threads_parts(objects, aws_config, dir, input)
    elseif input.DownloadStrategy == "keep off thread 1, no parts"
        objects -> download_objects_keep_off_thread_1_no_parts(objects, aws_config, dir, input)
    else
        error("unknown download strategy: $(input.DownloadStrategy)")
    end

    objects = JSON.parsefile(input.ObjectsPath)
    Base.CoreLogging.with_logstate(Base.CoreLogging.LogState(Base.CoreLogging.Error, Base.current_logger())) do
        try
            # warmup
            n = min(Threads.nthreads() * 2, length(objects))
            do_benchmark(objects[1:n], input, download_strategy)

            # run
            output = do_benchmark(objects, input, download_strategy)
            println(json(output))
        finally
            rm(dir; recursive = true, force = true)
        end
    end
end

main()
