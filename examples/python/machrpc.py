# python3 -m grpc_tools.protoc \
#     -I ../../machrpc \
#     --python_out=. \
#     --grpc_python_out=. \
#     ../../machrpc/machrpc.proto


from google.protobuf.any_pb2 import Any
import google.protobuf.timestamp_pb2 as pb_ts
import google.protobuf.wrappers_pb2 as pb_wp
import time
from datetime import datetime
import grpc
import machrpc_pb2_grpc
import machrpc_pb2

channel = grpc.insecure_channel('127.0.0.1:5655')
mach_stub = machrpc_pb2_grpc.MachbaseStub(channel)
sqlText = "select * from example order by time limit 10"
rsp = mach_stub.Query(machrpc_pb2.QueryRequest(sql=sqlText))
cols = mach_stub.Columns(rsp.rowsHandle)
if cols.success:
    header = ['RowNum']
    for c in cols.columns:
        header.append(f"{c.name}({c.type})  ")
    print('   '.join(header))
nrow = 0
while True:
    fetch = mach_stub.RowsFetch(rsp.rowsHandle)
    if fetch.hasNoRows:
        break
    nrow+=1
    line = []
    line.append(str(nrow))
    for i, c in enumerate(cols.columns):
        v = fetch.values[i]
        if c.type == "string":
            line.append(convpb(v))
        elif c.type == "datetime":
            line.append(convpb(v))
        elif c.type == "double":
            line.append(convpb(v))
        else:
            line.append(f"unknown {str(v)}")
    print('     '.join(line))
_ = mach_stub.RowsClose(rsp.rowsHandle)

def convpb(v):
    if v.type_url == "type.googleapis.com/google.protobuf.StringValue":
        r = pb_wp.StringValue()
        v.Unpack(r)
        return r.value
    elif v.type_url == "type.googleapis.com/google.protobuf.Timestamp":
        r = pb_ts.Timestamp()
        v.Unpack(r)
        dt = datetime.fromtimestamp(r.seconds)
        return dt.strftime('%Y-%m-%d %H:%M:%S')
    elif v.type_url == "type.googleapis.com/google.protobuf.DoubleValue":
        r = pb_wp.DoubleValue()
        v.Unpack(r)
        return str(r.value)
