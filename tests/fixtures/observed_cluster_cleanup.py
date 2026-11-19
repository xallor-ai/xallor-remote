"""Cleanup observed test owners; uncertainty retains profiles instead of guessing."""


def cleanup_observed_cluster(cluster, references, report):
    if cluster is None:
        return
    for name in cluster.profiles:
        try:
            cluster.stop(name)
        except Exception as error:
            report["cleanup_errors"].append(str(error))
    if references is not None:
        try:
            references.close_after_exit()
        except Exception as error:
            report["cleanup_errors"].append(str(error))
    try:
        if not report["cleanup_errors"]:
            cluster.close()
            report["cleanup_complete"] = cluster.cleanup_complete
        else:
            if cluster.relay and cluster.relay.poll() is None:
                cluster.relay.terminate()
                cluster.relay.wait(timeout=10)
            if cluster.log:
                cluster.log.close()
            report["retained_root"] = str(cluster.root)
    except Exception as error:
        report["cleanup_errors"].append(str(error))
