package network

//#include <stdio.h>
//#include <stdlib.h>
//#include <string.h>
//#include <unistd.h>
//#include <sys/types.h>
//#include <sys/socket.h>
//#include <netinet/in.h>
//#include <arpa/inet.h>
//#include <netdb.h>
//#include <fcntl.h>
//#include <errno.h>
/*


int cListenPort(char* ip, int port, int timeout) {

    struct sockaddr_in address;
    fd_set fdset;
    short int sock = -1;
    struct timeval tv;
    char *addr;

    addr = ip;

    address.sin_family = AF_INET;
    address.sin_addr.s_addr = inet_addr(addr);
    address.sin_port = htons(port);

    sock = socket(AF_INET, SOCK_STREAM, 0);
    //printf("%d sock\n", sock);
    if (sock <= 0) {
        return -1;
    }
    fcntl(sock, F_SETFL, O_NONBLOCK);

    int connErr = -2;
    connErr = connect(sock, (struct sockaddr *)&address, sizeof(address));
    if ((connErr != 0 ) && (errno != EINPROGRESS)){
        close(sock);
        return -1;
    }

    //printf("connErr %d\n", connErr);
    //printf("errno %d\n", errno);

    //if (connErr < 0){
    //    return -210;
    //}
    FD_ZERO(&fdset);
    FD_SET(sock, &fdset);

    tv.tv_sec = timeout;
    tv.tv_usec = 0;

    int errSelect = -2;
    errSelect = select(sock + 1, NULL, &fdset, NULL, &tv);
    if (errSelect == 1)
    {
        int so_error;
        socklen_t len = sizeof so_error;

        getsockopt(sock, SOL_SOCKET, SO_ERROR, &so_error, &len);
        if (so_error == 0) {
            //printf("%s:%d is open\n", addr, port);
            close(sock);
            return 0;
        }
    } else {
        //printf("%d\n", errSelect);
        //printf("%s:%d is not open\n", addr, port);
        close(sock);
        return -1;
    }

    close(sock);
    return -1;
}

int sendAndRcvMsg(char *ipaddr,
					int port,
			      	int sockType,
					int protoType,
					char *pcPkt,
					int pktLen,
					char *pcBuf,
					int len){

    int sockfd, numbytes;
    struct sockaddr_in peer_addr;
    int ret;
    struct timeval timeout = {3,0};

    if ((sockfd = socket(AF_INET, sockType, protoType)) == -1)
    {
        return -1;
    }

    ret = setsockopt(sockfd, SOL_SOCKET, SO_RCVTIMEO, (char *)&timeout, sizeof(timeout));
    ret |= setsockopt(sockfd, SOL_SOCKET, SO_SNDTIMEO, (char *)&timeout, sizeof(timeout));
    if (ret != 0) {
        close(sockfd);
        return -2;
    }

    memset(&peer_addr, 0, sizeof(peer_addr));
    peer_addr.sin_family = AF_INET;
    peer_addr.sin_port = htons(port);
    peer_addr.sin_addr.s_addr = inet_addr(ipaddr);
    if (connect(sockfd, (struct sockaddr *)&peer_addr, sizeof(struct sockaddr)) == -1)
    {
	close(sockfd);
        return -3;
    }

    if (send(sockfd, pcPkt, pktLen, 0) == -1) {
	close(sockfd);
        return -4;
    }

    if ((numbytes=recv(sockfd, pcBuf, len, 0)) == -1)
    {
        close(sockfd);
        return -5;
    }

    close(sockfd);

    return numbytes;
}


*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	RCVBUF_LEN = 2048
)

func ListenPort(ip string, port int, time int) error {
	cip := C.CString(ip)
	defer func() {
		C.free(unsafe.Pointer(cip))
	}()
	ret := C.cListenPort(cip, C.int(port), C.int(time))
	if ret != 0 {
		return errors.New(fmt.Sprintf("%s:%d is not open.", ip, port))
	} else {
		return nil
	}
}

func SendTcpWithC(ipaddr string, port int, pkt string) ([]byte, error) {

	portC := C.int(port)
	bufLenC := C.int(RCVBUF_LEN)

	rcvBuf := make([]byte, RCVBUF_LEN)
	rcvBufC := (*C.char)(unsafe.Pointer(&rcvBuf[0]))

	ipaddrC := C.CString(ipaddr)
	pktC := C.CString(pkt)
	defer func() {
		C.free(unsafe.Pointer(pktC))
		C.free(unsafe.Pointer(ipaddrC))
	}()

	ret := int(C.sendAndRcvMsg(ipaddrC,
		portC,
		C.int(syscall.SOCK_STREAM),
		C.int(syscall.IPPROTO_TCP),
		pktC,
		C.int(len(pkt)),
		rcvBufC,
		bufLenC))
	//fmt.Println("ret: ", ret, string(rcvBuf[:]))
	if ret > 0 {
		return rcvBuf[:ret], nil
	}

	return nil, errors.New("send msg failed, code=" + strconv.Itoa(ret))
}

func SendIcmpWithC(ipaddr string, pkt string) ([]byte, error) {

	portC := C.int(0)
	bufLenC := C.int(RCVBUF_LEN)

	rcvBuf := make([]byte, RCVBUF_LEN)
	rcvBufC := (*C.char)(unsafe.Pointer(&rcvBuf[0]))

	ipaddrC := C.CString(ipaddr)
	pktC := C.CString(pkt)
	defer func() {
		C.free(unsafe.Pointer(pktC))
		C.free(unsafe.Pointer(ipaddrC))
	}()

	ret := int(C.sendAndRcvMsg(ipaddrC,
		portC,
		C.int(syscall.SOCK_RAW),
		C.int(syscall.IPPROTO_ICMP),
		pktC,
		C.int(len(pkt)),
		rcvBufC,
		bufLenC))
	//fmt.Println("ret: ", ret, string(rcvBuf[:]))
	if ret > 0 {
		return rcvBuf[:ret], nil
	}

	return nil, errors.New("send msg failed")
}
