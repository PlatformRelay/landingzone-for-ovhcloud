#!/usr/bin/python3.14 -I
"""Credential-free preparation; install only after independent digest approval.

This source is data in the candidate. The fixed reviewed build driver copies it
outside the checkout before execution. It never runs candidate Task/config/code.
"""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import selectors
import stat
import subprocess
import tarfile
import time

IMAGE = 'cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26'
PROVIDER_URL = 'https://github.com/ovh/terraform-provider-ovh/releases/download/v2.21.0/terraform-provider-ovh_2.21.0_linux_amd64.zip'
PROVIDER_SHA = '54dc88d0e863d9f87e9a9c63407f9d9e6a73c0f29d16b226f9bafc6fd2507712'
ARCHIVES = {
    'go1.27.1.linux-amd64.tar.gz': '63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445',
    'tofu_1.13.0_linux_amd64.tar.gz': '1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb',
    'terramate_0.17.3_linux_x86_64.tar.gz': '303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060',
}
def required_path(name):
    value = os.environ.get(name, '')
    if not value.startswith('/'):
        raise SystemExit(name + ': absolute host path required')
    return Path(value)

# Host locations are supplied by the external driver, never by the candidate.
ROOT = required_path('LZ_PREP_ROOT')            # private preparation root
OLD = required_path('LZ_TOOLCHAIN_ROOT')        # qualified T001 toolchain root
CRANE = required_path('LZ_CRANE')               # crane binary for image export
TASK = required_path('LZ_TASK')                 # Task binary placed at /tcb/task

def digest(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise RuntimeError('single-link regular file required: ' + str(path))
    return hashlib.sha256(path.read_bytes()).hexdigest()

def inventory(root):
    result = {}
    for path in [root / 'resources', *sorted((root / 'resources').rglob('*'))]:
        info = path.lstat()
        entry = {'mode': stat.S_IMODE(info.st_mode), 'kind': '', 'value': ''}
        if stat.S_ISDIR(info.st_mode):
            entry['kind'] = 'directory'
        elif stat.S_ISREG(info.st_mode):
            entry.update(kind='file', value=digest(path))
        elif stat.S_ISLNK(info.st_mode):
            target = os.readlink(path)
            image = root / 'resources/rootfs'
            if not path.is_relative_to(image):
                raise RuntimeError('link outside image')
            resolved = Path(os.path.normpath(str(image / target.lstrip('/')) if target.startswith('/') else str(path.parent / target)))
            if not resolved.is_relative_to(image):
                raise RuntimeError('escaping image link')
            entry.update(kind='symlink', value=target)
        else:
            raise RuntimeError('unsupported prepared file type')
        result[path.relative_to(root).as_posix()] = entry
    return result

def copy_go(destination):
    source = OLD / 'tools/go/go'
    expected = {}
    for line in (ROOT / 'proof/go-files.sha256').read_text().splitlines():
        checksum, name = line.split('  ', 1)
        path = Path(name)
        if not path.is_relative_to(source) or path in expected:
            raise RuntimeError('bounded unique Go manifest path required')
        expected[path] = checksum
    actual = set()
    for parent, directories, files in os.walk(source, followlinks=False):
        for name in directories:
            if not stat.S_ISDIR((Path(parent) / name).lstat().st_mode):
                raise RuntimeError('Go directory link/type refused')
        for name in files:
            path = Path(parent) / name
            if path not in expected or digest(path) != expected[path]:
                raise RuntimeError('Go extra/link/hash refused')
            actual.add(path)
    if actual != set(expected):
        raise RuntimeError('Go complete inventory differs')
    destination.mkdir()
    # Only manifest-listed regular files can be copied, even if a concurrent
    # extra entry appears after admission. Never follow a link during the copy.
    for path, checksum in expected.items():
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, 'rb') as source_file:
            info = os.fstat(source_file.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise RuntimeError('Go copy type/link differs')
            content = source_file.read()
        if hashlib.sha256(content).hexdigest() != checksum:
            raise RuntimeError('Go copy checksum differs')
        target = destination / path.relative_to(source)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        target.chmod(stat.S_IMODE(info.st_mode))

def export_image(environment, private):
    # Crane supports stdout export (v0.22.0 cmd/crane/cmd/export.go). Pipe
    # backpressure plus pre-write counters bound disk use before accumulation.
    process = subprocess.Popen([str(CRANE), 'export', '--platform=linux/amd64', IMAGE, '-'],
        env=environment, cwd=private, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    deadline = time.monotonic() + 120
    selector = selectors.DefaultSelector()
    counts = {'stdout':0, 'stderr':0}
    limits = {'stdout':256*1024*1024, 'stderr':1024*1024}
    try:
        with (private / 'image.tar').open('xb') as archive, (private / 'export.stderr').open('xb') as diagnostic:
            destinations = {'stdout':archive, 'stderr':diagnostic}
            selector.register(process.stdout, selectors.EVENT_READ, 'stdout')
            selector.register(process.stderr, selectors.EVENT_READ, 'stderr')
            while selector.get_map():
                if time.monotonic() >= deadline:
                    raise RuntimeError('image export deadline exceeded')
                for key, _ in selector.select(timeout=min(1, deadline-time.monotonic())):
                    content = os.read(key.fileobj.fileno(), 65536)
                    if not content:
                        selector.unregister(key.fileobj)
                        continue
                    counts[key.data] += len(content)
                    if counts[key.data] > limits[key.data]:
                        raise RuntimeError('image export byte limit exceeded: ' + key.data)
                    destinations[key.data].write(content)
            code = process.wait(timeout=max(0.01, deadline-time.monotonic()))
            if code != 0:
                raise RuntimeError('image export failed: ' + str(code))
    finally:
        selector.close()
        if process.poll() is None:
            process.kill()
        process.wait(timeout=10)
        process.stdout.close()
        process.stderr.close()

def unpack_image(archive, destination):
    # No archive path/link/device can escape staging. Links are created last so
    # extraction never traverses one. Hardlinks/devices/whiteouts are rejected.
    with tarfile.open(archive, mode='r|', stream=True) as tar:
        seen, links, directories = set(), [], []
        count, expanded = 0, 0
        for member in tar:
            count += 1
            expanded += member.size
            if count > 50000 or member.size < 0 or expanded > 256 * 1024 * 1024 or len(member.name) > 4096 or len(member.linkname) > 4096:
                raise RuntimeError('image archive limits exceeded')
            name = PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts or not name.parts:
                raise RuntimeError('unrooted image member')
            normalized = name.as_posix()
            if normalized in seen:
                raise RuntimeError('duplicate image member')
            seen.add(normalized)
            # The runtime always creates a private /dev with bwrap. Preserve
            # signed export provenance, but omit image /dev from this projection;
            # never materialize archived devices, sockets or FIFOs on the host.
            # Path normalization and every resource counter precede exclusion.
            if name.parts[0] == 'dev':
                continue
            target = destination / normalized
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                directories.append((target, (member.mode & 0o755) | 0o500))
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                with tar.extractfile(member) as source, target.open('xb') as output:
                    shutil.copyfileobj(source, output)
                target.chmod((member.mode & 0o755) | 0o400)
            elif member.issym():
                resolved = Path(os.path.normpath(str(destination / member.linkname.lstrip('/')) if member.linkname.startswith('/') else str(target.parent / member.linkname)))
                if not resolved.is_relative_to(destination):
                    raise RuntimeError('escaping image link')
                links.append((target, member.linkname))
            else:
                raise RuntimeError('non-file image member: name='+repr(member.name)+' type='+repr(member.type)+' link='+repr(member.linkname))
        for target, link in links:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.symlink_to(link)
        # Materialize children while private directories remain owner-writable.
        # Apply final signed-image projection modes only after every file/link.
        for target, mode in sorted(directories, key=lambda item: len(item[0].parts), reverse=True):
            target.chmod(mode)

def remove_staging(stage):
    if stage != ROOT / 'staging' or not stat.S_ISDIR(stage.lstat().st_mode):
        raise RuntimeError('cleanup limited to owned staging directory')
    # Final image modes can be read/traverse-only. Restore owner access solely
    # inside this owned extraction tree before removing it; never follow links.
    for parent, _, _ in os.walk(stage, topdown=True, followlinks=False):
        path = Path(parent)
        if not stat.S_ISDIR(path.lstat().st_mode):
            raise RuntimeError('cleanup real directory required')
        path.chmod(stat.S_IMODE(path.lstat().st_mode) | 0o700)
    shutil.rmtree(stage)

def prepare():
    if Path(__file__) != ROOT / 'proof/prepare.py':
        raise RuntimeError('fixed external preparation required')
    os.umask(0o077)
    final, stage = ROOT / 'bundle', ROOT / 'staging'
    if final.exists() or stage.exists():
        raise RuntimeError('absent private destinations required')
    stage.mkdir(mode=0o700)
    try:
        resources = stage / 'resources'
        resources.mkdir()
        for name, expected in ARCHIVES.items():
            if digest(OLD / 'inputs' / name) != expected:
                raise RuntimeError('qualified artifact differs: ' + name)
        # The approved build driver compares the entire prepared Go tree and
        # helper bytes before this preparation. Copies do not mutate T001.
        copy_go(resources / 'go')
        shutil.copyfile(OLD / 'tools/tofu/tofu', resources / 'tofu')
        shutil.copyfile(OLD / 'tools/terramate/terramate', resources / 'terramate')
        shutil.copyfile(TASK, resources / 'task')
        for name in ('task', 'tofu', 'terramate'):
            (resources / name).chmod(0o700)
        private = stage / 'transport'
        private.mkdir()
        environment = {'PATH': '/usr/bin:/bin', 'HOME': str(private), 'DOCKER_CONFIG': str(private), 'XDG_CACHE_HOME': str(private)}
        # The OCI reference is immutable. Crane performs registry-content digest
        # verification; the export's bytes and extracted inventory are retained.
        export_image(environment, private)
        image = resources / 'rootfs'
        image.mkdir()
        unpack_image(private / 'image.tar', image)
        for name in ('proc', 'dev', 'tmp', 'run', 'home', 'tcb', 'tools', 'mirror', 'candidate'):
            (image / name).mkdir(exist_ok=True)
        mirror = resources / 'mirror/registry.opentofu.org/ovh/ovh'
        mirror.mkdir(parents=True)
        package = mirror / 'terraform-provider-ovh_2.21.0_linux_amd64.zip'
        subprocess.run(['/usr/bin/curl', '--disable', '--fail', '--silent', '--show-error', '--location', '--proto', '=https', '--max-time', '60', '--max-filesize', '14000000', '--output', str(package), PROVIDER_URL], env=environment, cwd=private, check=True, timeout=65)
        if digest(package) != PROVIDER_SHA:
            raise RuntimeError('provider package checksum differs')
        (resources / 'tofurc').write_text('provider_installation {\n  filesystem_mirror {\n    path = "/mirror"\n    include = ["registry.opentofu.org/ovh/ovh"]\n  }\n}\n')
        prepared = {'Versions': {'go':'1.27.1','tofu':'1.13.0','terramate':'0.17.3'}, 'Artifacts':dict(zip(('go','tofu','terramate'),ARCHIVES.values())), 'Image':IMAGE, 'IsolationProof':'runtime-qualified'}
        manifest = {'prepared':prepared, 'files':inventory(stage), 'bwrap':'/usr/bin/bwrap','bwrap_sha256':digest(Path('/usr/bin/bwrap'))}
        (stage / 'resources.json').write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n')
        receipt = {'image':IMAGE,'image_export_sha256':digest(private / 'image.tar'),'image_projection':'rooted filesystem without image /dev; runtime supplies private bwrap /dev; privileged mode bits stripped; owner read added to regular files and owner read/traverse added to directories for resource admission','provider_sha256':digest(package),'resources_sha256':digest(stage / 'resources.json')}
        shutil.rmtree(private)
        stage.rename(final)
        (ROOT / 'prepared.json').write_text(json.dumps(receipt,sort_keys=True,indent=2)+'\n')
    finally:
        if stage.exists():
            remove_staging(stage)

if __name__ == '__main__':
    prepare()
